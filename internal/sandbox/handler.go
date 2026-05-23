package sandbox

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/quota"
)

// ActiveSandboxCounter abstracts the per-tenant count used by quota gating
// so the handler doesn't take a hard repo dep (test seam).
type ActiveSandboxCounter interface {
	CountActiveByTenant(ctx context.Context, tenantID uuid.UUID) (int, error)
}

// Handler exposes the sandbox Runtime as HTTP endpoints.
type Handler struct {
	rt           Runtime
	audit        audit.Sink
	quota        *quota.Service
	activeCount  ActiveSandboxCounter
	snapshotRepo *SnapshotRepo
}

func NewHandler(rt Runtime) *Handler { return &Handler{rt: rt} }

// WithSnapshotRepo wires the slice-22b snapshot repository so the snapshot
// list / get routes can read persisted metadata directly without an extra
// Runtime trip. nil leaves slice-22b read APIs disabled (handler will 503).
func (h *Handler) WithSnapshotRepo(repo *SnapshotRepo) *Handler {
	h.snapshotRepo = repo
	return h
}

// WithAuditSink wires an audit.Sink so the handler records sandbox.create /
// sandbox.destroy entries on successful operations. Returns the receiver for
// chaining. Setter (rather than constructor arg) avoids breaking existing
// NewHandler callers in tests that don't care about audit.
func (h *Handler) WithAuditSink(s audit.Sink) *Handler {
	h.audit = s
	return h
}

// WithQuota wires a per-tenant active-sandbox cap. The handler will reject
// create requests with HTTP 429 when the count of non-terminal sandboxes
// for the tenant reaches the cap. nil quota or zero cap disables.
func (h *Handler) WithQuota(q *quota.Service, counter ActiveSandboxCounter) *Handler {
	h.quota = q
	h.activeCount = counter
	return h
}

// Register mounts /sandbox/* routes on rg. rg should already have
// auth.Middleware applied (handler relies on auth.FromCtx for claims).
func (h *Handler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/sandbox/sessions")
	g.POST("", h.create)
	g.GET("/:id", h.get)
	g.DELETE("/:id", h.destroy)
	g.POST("/:id/exec", h.exec)
	g.GET("/:id/files", h.files)
	g.PUT("/:id/files", h.writeFile)
	g.POST("/:id/snapshot", h.snapshot)
	// Slice 22b — snapshot read APIs. Always registered so the WebUI can
	// distinguish "off" (503 snapshot_disabled) from "broken".
	s := rg.Group("/sandbox/snapshots")
	s.GET("", h.listSnapshots)
	s.POST("/restore/:id", h.restoreSnapshot)
	s.GET("/:id", h.getSnapshot)
}

func (h *Handler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return cl, true
}

func (h *Handler) parseID(c *gin.Context) (uuid.UUID, bool) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: id"})
		return uuid.Nil, false
	}
	return id, true
}

type createReq struct {
	Image     string             `json:"image,omitempty"`
	ProjectID *string            `json:"project_id,omitempty"`
	Resources *resourceLimitsDTO `json:"resources,omitempty"`
	Network   string             `json:"network,omitempty"`
	Env       map[string]string  `json:"env,omitempty"`
	Labels    map[string]string  `json:"labels,omitempty"`
}

type resourceLimitsDTO struct {
	CPUs      float64 `json:"cpus,omitempty"`
	MemoryMB  int64   `json:"memory_mb,omitempty"`
	PIDsLimit int64   `json:"pids_limit,omitempty"`
}

type sandboxDTO struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	OwnerUserID uuid.UUID  `json:"owner_user_id"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	Status      string     `json:"status"`
	Image       string     `json:"image"`
	NetworkMode string     `json:"network_mode"`
	CPUs        float64    `json:"cpus"`
	MemoryMB    int64      `json:"memory_mb"`
	PIDsLimit   int64      `json:"pids_limit"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DestroyedAt *time.Time `json:"destroyed_at,omitempty"`
}

func toDTO(sb *Sandbox) sandboxDTO {
	return sandboxDTO{
		ID:          sb.ID,
		TenantID:    sb.TenantID,
		OwnerUserID: sb.OwnerUserID,
		ProjectID:   sb.ProjectID,
		Status:      string(sb.Status),
		Image:       sb.Image,
		NetworkMode: string(sb.Network),
		CPUs:        sb.Resources.CPUs,
		MemoryMB:    sb.Resources.MemoryMB,
		PIDsLimit:   sb.Resources.PIDsLimit,
		CreatedAt:   sb.CreatedAt,
		UpdatedAt:   sb.UpdatedAt,
		DestroyedAt: sb.DestroyedAt,
	}
}

func (h *Handler) create(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	if h.quota != nil && h.activeCount != nil {
		cap := h.quota.SandboxCap()
		if cap > 0 {
			n, err := h.activeCount.CountActiveByTenant(c.Request.Context(), cl.TenantID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "quota_check_failed"})
				return
			}
			if n >= cap {
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "quota_exceeded", "kind": "sandbox.active", "used": n, "cap": cap})
				return
			}
		}
	}
	var req createReq
	_ = c.ShouldBindJSON(&req) // 全字段 optional,body 可为空

	opts := CreateOpts{
		TenantID:    cl.TenantID,
		OwnerUserID: cl.UserID,
		Image:       req.Image,
		Network:     NetworkMode(req.Network),
		Env:         req.Env,
		Labels:      req.Labels,
	}
	if req.Resources != nil {
		opts.Resources = ResourceLimits{
			CPUs:      req.Resources.CPUs,
			MemoryMB:  req.Resources.MemoryMB,
			PIDsLimit: req.Resources.PIDsLimit,
		}
	}
	if req.ProjectID != nil {
		pid, err := uuid.Parse(*req.ProjectID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: project_id"})
			return
		}
		opts.ProjectID = &pid
	}

	start := time.Now()
	sb, err := h.rt.Create(c.Request.Context(), opts)
	if err != nil {
		if isValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		return
	}
	h.auditSandboxEvent(c, start, sb, "sandbox.create", http.StatusCreated, map[string]any{
		"image": sb.Image,
	})
	c.JSON(http.StatusCreated, toDTO(sb))
}

func (h *Handler) auditSandboxEvent(c *gin.Context, start time.Time, sb *Sandbox, action string, status int, meta map[string]any) {
	if h.audit == nil {
		return
	}
	tid := sb.TenantID
	uid := sb.OwnerUserID
	audit.Detached(h.audit, audit.Entry{
		OccurredAt: start,
		TenantID:   &tid, UserID: &uid,
		Action: action,
		Target: sb.ID.String(),
		Method: c.Request.Method, Path: c.FullPath(),
		Status:     status,
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata:   meta,
	}, nil)
}

func (h *Handler) get(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	sb, err := h.rt.Get(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		if errors.Is(err, ErrSandboxNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		return
	}
	c.JSON(http.StatusOK, toDTO(sb))
}

func (h *Handler) destroy(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	start := time.Now()
	err := h.rt.Destroy(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		if errors.Is(err, ErrSandboxNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		return
	}
	h.auditSandboxEvent(c, start,
		&Sandbox{ID: id, TenantID: cl.TenantID, OwnerUserID: cl.UserID},
		"sandbox.destroy", http.StatusNoContent, nil)
	c.Status(http.StatusNoContent)
}

type execReq struct {
	Cmd         []string          `json:"cmd"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	StdinBase64 string            `json:"stdin_base64,omitempty"`
	TimeoutSec  int               `json:"timeout_sec,omitempty"`
}

type execResp struct {
	ExitCode     int    `json:"exit_code"`
	StdoutBase64 string `json:"stdout_base64"`
	StderrBase64 string `json:"stderr_base64"`
	Truncated    bool   `json:"truncated"`
	DurationMS   int64  `json:"duration_ms"`
	TimedOut     bool   `json:"timed_out"`
}

func (h *Handler) exec(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	var req execReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}

	opts := ExecOpts{
		Cmd:        req.Cmd,
		WorkingDir: req.WorkingDir,
		Env:        req.Env,
		TimeoutSec: req.TimeoutSec,
	}
	if req.StdinBase64 != "" {
		b, err := base64.StdEncoding.DecodeString(req.StdinBase64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: stdin_base64"})
			return
		}
		opts.Stdin = b
	}

	res, err := h.rt.Exec(c.Request.Context(), cl.TenantID, id, opts)
	if err != nil {
		switch {
		case errors.Is(err, ErrSandboxNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, ErrSandboxNotReady):
			c.JSON(http.StatusConflict, gin.H{"error": "not_ready"})
		case isValidationError(err):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		}
		return
	}
	c.JSON(http.StatusOK, execResp{
		ExitCode:     res.ExitCode,
		StdoutBase64: base64.StdEncoding.EncodeToString(res.Stdout),
		StderrBase64: base64.StdEncoding.EncodeToString(res.Stderr),
		Truncated:    res.Truncated,
		DurationMS:   res.DurationMS,
		TimedOut:     res.TimedOut,
	})
}

type writeFileReq struct {
	ContentBase64 string `json:"content_base64"`
}

func (h *Handler) writeFile(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	rel := c.Query("path")
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: path"})
		return
	}
	var req writeFileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: content_base64"})
		return
	}
	if err := h.rt.WriteFile(c.Request.Context(), cl.TenantID, id, rel, data); err != nil {
		fileErrToHTTP(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

const filePreviewMaxBytes = 256 * 1024

func (h *Handler) files(c *gin.Context) {
	if c.Query("list") == "1" || strings.EqualFold(c.Query("list"), "true") {
		h.listFiles(c)
		return
	}
	h.readFile(c)
}

func (h *Handler) listFiles(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	rel := c.Query("path")
	if rel == "" {
		rel = "."
	}
	entries, err := ListDir(c.Request.Context(), h.rt, cl.TenantID, id, rel)
	if err != nil {
		switch {
		case errors.Is(err, ErrSandboxNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, ErrSandboxNotReady):
			c.JSON(http.StatusConflict, gin.H{"error": "not_ready"})
		case isValidationError(err):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries})
}

func (h *Handler) readFile(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	rel := c.Query("path")
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: path"})
		return
	}
	data, err := h.rt.ReadFile(c.Request.Context(), cl.TenantID, id, rel)
	if err != nil {
		fileErrToHTTP(c, err)
		return
	}
	if len(data) > filePreviewMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_too_large"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"content_base64": base64.StdEncoding.EncodeToString(data),
		"size":           len(data),
	})
}

// snapshotDTO is the JSON surface of a sandbox snapshot. SessionID may be
// nil after the original sandbox was destroyed (FK ON DELETE SET NULL).
type snapshotDTO struct {
	ID        uuid.UUID      `json:"id"`
	TenantID  uuid.UUID      `json:"tenant_id"`
	UserID    uuid.UUID      `json:"user_id"`
	SessionID *uuid.UUID     `json:"session_id"`
	ObjectKey string         `json:"object_key"`
	SizeBytes int64          `json:"size_bytes"`
	ImageRef  string         `json:"image_ref"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

func toSnapshotDTO(s *Snapshot) snapshotDTO {
	return snapshotDTO{
		ID: s.ID, TenantID: s.TenantID, UserID: s.UserID,
		SessionID: s.SessionID, ObjectKey: s.ObjectKey,
		SizeBytes: s.SizeBytes, ImageRef: s.ImageRef,
		Metadata: s.Metadata, CreatedAt: s.CreatedAt,
	}
}

func (h *Handler) snapshot(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	start := time.Now()
	snap, err := h.rt.Snapshot(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		switch {
		case errors.Is(err, ErrSnapshotDisabled):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "snapshot_disabled"})
		case errors.Is(err, ErrSandboxNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, ErrSandboxNotReady):
			c.JSON(http.StatusConflict, gin.H{"error": "not_ready"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		}
		return
	}
	h.auditSnapshotEvent(c, start, snap.TenantID, snap.UserID, "sandbox.snapshot.create",
		snap.ID.String(), http.StatusCreated, map[string]any{
			"object_key": snap.ObjectKey,
			"size_bytes": snap.SizeBytes,
			"image_ref":  snap.ImageRef,
			"session_id": snap.SessionID,
		})
	c.JSON(http.StatusCreated, toSnapshotDTO(snap))
}

const snapshotListMaxLimit = 200
const snapshotListDefaultLimit = 50

func (h *Handler) listSnapshots(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	if h.snapshotRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "snapshot_disabled"})
		return
	}
	var sessionID *uuid.UUID
	if v := c.Query("session_id"); v != "" {
		sid, err := uuid.Parse(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: session_id"})
			return
		}
		sessionID = &sid
	}
	limit := snapshotListDefaultLimit
	if v := c.Query("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
			if limit > snapshotListMaxLimit {
				limit = snapshotListMaxLimit
			}
		}
	}
	start := time.Now()
	rows, err := h.snapshotRepo.List(c.Request.Context(), cl.TenantID, sessionID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		return
	}
	items := make([]snapshotDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toSnapshotDTO(&rows[i]))
	}
	meta := map[string]any{"count": len(items)}
	if sessionID != nil {
		meta["session_id_filter"] = sessionID.String()
	}
	h.auditSnapshotEvent(c, start, cl.TenantID, cl.UserID, "sandbox.snapshot.list",
		"", http.StatusOK, meta)
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) getSnapshot(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	if h.snapshotRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "snapshot_disabled"})
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	start := time.Now()
	snap, err := h.snapshotRepo.Get(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		if errors.Is(err, ErrSnapshotNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		return
	}
	h.auditSnapshotEvent(c, start, snap.TenantID, snap.UserID, "sandbox.snapshot.get",
		snap.ID.String(), http.StatusOK, map[string]any{})
	c.JSON(http.StatusOK, toSnapshotDTO(snap))
}

func (h *Handler) restoreSnapshot(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	start := time.Now()
	sb, err := h.rt.RestoreFromSnapshot(c.Request.Context(), cl.TenantID, cl.UserID, id)
	if err != nil {
		switch {
		case errors.Is(err, ErrSnapshotDisabled):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "snapshot_disabled"})
		case errors.Is(err, ErrSnapshotNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		}
		return
	}
	h.auditSnapshotEvent(c, start, sb.TenantID, sb.OwnerUserID, "sandbox.snapshot.restore",
		id.String(), http.StatusCreated, map[string]any{
			"sandbox_id": sb.ID.String(),
		})
	c.JSON(http.StatusCreated, toDTO(sb))
}

// auditSnapshotEvent mirrors auditSandboxEvent but takes explicit tenant/user
// (snapshots are not always tied to a Sandbox once session_id goes NULL).
func (h *Handler) auditSnapshotEvent(c *gin.Context, start time.Time,
	tenantID, userID uuid.UUID, action, target string, status int, meta map[string]any) {
	if h.audit == nil {
		return
	}
	tid := tenantID
	uid := userID
	audit.Detached(h.audit, audit.Entry{
		OccurredAt: start,
		TenantID:   &tid, UserID: &uid,
		Action: action,
		Target: target,
		Method: c.Request.Method, Path: c.FullPath(),
		Status:     status,
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata:   meta,
	}, nil)
}

func fileErrToHTTP(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrSandboxNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, ErrSandboxNotReady):
		c.JSON(http.StatusConflict, gin.H{"error": "not_ready"})
	case errors.Is(err, ErrPathOutsideWorkspace):
		c.JSON(http.StatusBadRequest, gin.H{"error": "path_outside_workspace"})
	case errors.Is(err, ErrTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "payload_too_large"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
	}
}

func isValidationError(err error) bool {
	// validate.go 用 fmt.Errorf("validation: ...") 包装
	if err == nil {
		return false
	}
	const prefix = "validation:"
	return len(err.Error()) >= len(prefix) && err.Error()[:len(prefix)] == prefix
}

package mcp

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
)

// AdminHandler exposes /admin/mcp-servers REST. The Gin router group passed to
// Register must already apply auth.Middleware + auth.RequireAdmin; this
// handler trusts auth.FromCtx(ctx) for tenant + user scoping.
type AdminHandler struct {
	mgr   *Manager
	repo  *Repo
	audit audit.Sink
}

// NewAdminHandler returns a handler bound to the live Manager and Repo. If
// mgr is nil (cfg.MCP.Enabled=false) every endpoint returns 503 — callers
// must still mount the routes so the WebUI gets a deterministic response.
func NewAdminHandler(mgr *Manager, repo *Repo, sink audit.Sink) *AdminHandler {
	return &AdminHandler{mgr: mgr, repo: repo, audit: sink}
}

// Register wires the eight REST verbs under /admin/mcp-servers. The route
// names mirror Skills/Workflows for muscle memory: collection POST/GET,
// item GET/PUT/DELETE, plus three side-effect actions (refresh/test +
// enable/disable). See plan §D8 for the full table.
func (h *AdminHandler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/admin/mcp-servers")
	g.POST("", h.create)
	g.GET("", h.list)
	g.GET("/:id", h.get)
	g.PUT("/:id", h.update)
	g.DELETE("/:id", h.delete)
	g.POST("/:id/refresh", h.refresh)
	g.POST("/:id/test", h.test)
	g.POST("/:id/enable", h.enable)
	g.POST("/:id/disable", h.disable)
}

type createReq struct {
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	URL         string            `json:"url"`
	AuthType    string            `json:"auth_type"`
	AuthToken   string            `json:"auth_token"`
	Headers     map[string]string `json:"headers"`
	Enabled     *bool             `json:"enabled"`
}

type updateReq struct {
	Name        *string            `json:"name"`
	Description *string            `json:"description"`
	URL         *string            `json:"url"`
	AuthType    *string            `json:"auth_type"`
	AuthToken   *string            `json:"auth_token"`
	Headers     *map[string]string `json:"headers"`
	Enabled     *bool              `json:"enabled"`
}

type testReq struct {
	URL       string            `json:"url"`
	AuthType  string            `json:"auth_type"`
	AuthToken string            `json:"auth_token"`
	Headers   map[string]string `json:"headers"`
}

// claims pulls tenant + user from the JWT context. Returns ok=false after
// writing 401 if no claims are present (auth.Middleware should have rejected
// already; defensive only).
func (h *AdminHandler) claims(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return uuid.Nil, uuid.Nil, false
	}
	return cl.TenantID, cl.UserID, true
}

// guard refuses every method when the Manager is nil (cfg.MCP.Enabled=false).
// Returns false when the handler should not proceed.
func (h *AdminHandler) guard(c *gin.Context) bool {
	if h.mgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "mcp_disabled"})
		return false
	}
	return true
}

func (h *AdminHandler) create(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	tid, uid, ok := h.claims(c)
	if !ok {
		return
	}
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if err := ValidateSlug(req.Slug); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_slug"})
		return
	}
	if req.Name == "" || req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name_and_url_required"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	s := &Server{
		TenantID: tid, Slug: req.Slug, Name: req.Name,
		Description: req.Description, URL: req.URL,
		AuthType: req.AuthType, AuthToken: req.AuthToken,
		Headers: req.Headers, Enabled: enabled,
	}
	created, err := h.repo.Insert(c.Request.Context(), s)
	if err != nil {
		if errors.Is(err, ErrSlugConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "slug_taken"})
			return
		}
		if errors.Is(err, ErrSlugInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_slug"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Fetch tools eagerly so the admin sees them in the response. If the
	// remote is down we keep the row but echo the error so the UI can show it.
	tools, regErr := h.mgr.RegisterServer(c.Request.Context(), created)
	if regErr == nil {
		created.ToolsCache = tools
		now := time.Now()
		created.LastSeenAt = &now
	} else {
		created.LastError = regErr.Error()
	}

	h.emitAudit(tid, uid, "mcp.admin.create", created.Slug, map[string]any{
		"server_id":   created.ID.String(),
		"url":         created.URL,
		"auth_type":   created.AuthType,
		"token_fp":    AuthTokenFingerprint(created.AuthToken),
		"tool_count":  len(tools),
		"bus_error":   errorString(regErr),
	})

	c.JSON(http.StatusCreated, redact(created))
}

func (h *AdminHandler) list(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	rows, err := h.repo.List(c.Request.Context(), tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]Server, len(rows))
	for i := range rows {
		rd := redact(&rows[i])
		out[i] = *rd
	}
	c.JSON(http.StatusOK, gin.H{"servers": out})
}

func (h *AdminHandler) get(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.repo.Get(c.Request.Context(), tid, id)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, redact(row))
}

func (h *AdminHandler) update(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	tid, uid, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req updateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	updated, err := h.repo.Update(c.Request.Context(), tid, id,
		req.Name, req.Description, req.URL, req.AuthType, req.AuthToken,
		req.Headers, req.Enabled)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// URL/auth changes invalidate the registered Client; re-register if the
	// server is still enabled, else drop it from the Bus.
	var busErr error
	if updated.Enabled {
		_, busErr = h.mgr.RegisterServer(c.Request.Context(), updated)
		if busErr == nil {
			// Refresh tools_cache in the response payload.
			fresh, _ := h.repo.Get(c.Request.Context(), tid, id)
			if fresh != nil {
				updated = fresh
			}
		}
	} else {
		h.mgr.UnregisterServer(id)
	}

	h.emitAudit(tid, uid, "mcp.admin.update", updated.Slug, map[string]any{
		"server_id":  updated.ID.String(),
		"fields":     changedFields(req),
		"token_fp":   AuthTokenFingerprint(updated.AuthToken),
		"bus_error":  errorString(busErr),
	})

	c.JSON(http.StatusOK, redact(updated))
}

func (h *AdminHandler) delete(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	tid, uid, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.repo.Get(c.Request.Context(), tid, id)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.Delete(c.Request.Context(), tid, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.mgr.UnregisterServer(id)

	h.emitAudit(tid, uid, "mcp.admin.delete", row.Slug, map[string]any{
		"server_id": id.String(),
	})
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) refresh(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	tid, uid, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	tools, err := h.mgr.RefreshTools(c.Request.Context(), tid, id)
	meta := map[string]any{"server_id": id.String(), "tool_count": len(tools)}
	if err != nil {
		meta["error"] = err.Error()
	}
	row, getErr := h.repo.Get(c.Request.Context(), tid, id)
	if getErr == nil {
		h.emitAudit(tid, uid, "mcp.admin.refresh", row.Slug, meta)
	} else {
		h.emitAudit(tid, uid, "mcp.admin.refresh", id.String(), meta)
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

func (h *AdminHandler) test(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	// Body overrides DB lookup: letting admins probe a candidate URL before
	// save is the common case. If no body supplied AND :id resolves to an
	// existing row, fall back to the stored config so the "Test" button on
	// an existing row works without payload duplication.
	var req testReq
	_ = c.ShouldBindJSON(&req) // body is optional; bind errors are non-fatal
	if req.URL == "" {
		id, idOK := tryParseID(c)
		if idOK {
			row, err := h.repo.Get(c.Request.Context(), tid, id)
			if err == nil {
				req.URL = row.URL
				req.AuthType = row.AuthType
				req.AuthToken = row.AuthToken
				req.Headers = row.Headers
			}
		}
	}
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url_required"})
		return
	}
	probe := &Server{
		TenantID: tid, URL: req.URL,
		AuthType: req.AuthType, AuthToken: req.AuthToken,
		Headers: req.Headers,
	}
	if err := h.mgr.TestConnection(c.Request.Context(), probe); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"ok": false, "error": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AdminHandler) enable(c *gin.Context)  { h.setEnabled(c, true) }
func (h *AdminHandler) disable(c *gin.Context) { h.setEnabled(c, false) }

func (h *AdminHandler) setEnabled(c *gin.Context, enabled bool) {
	if !h.guard(c) {
		return
	}
	tid, uid, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	updated, err := h.repo.Update(c.Request.Context(), tid, id,
		nil, nil, nil, nil, nil, nil, &enabled)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var busErr error
	if enabled {
		_, busErr = h.mgr.RegisterServer(c.Request.Context(), updated)
	} else {
		h.mgr.UnregisterServer(id)
	}
	action := "mcp.admin.disable"
	if enabled {
		action = "mcp.admin.enable"
	}
	h.emitAudit(tid, uid, action, updated.Slug, map[string]any{
		"server_id": id.String(),
		"bus_error": errorString(busErr),
	})
	c.JSON(http.StatusOK, redact(updated))
}

// emitAudit centralizes the audit.Detached call so every endpoint writes a
// consistent envelope. errors from the sink are swallowed.
func (h *AdminHandler) emitAudit(tenantID, userID uuid.UUID, action, target string, meta map[string]any) {
	if h.audit == nil {
		return
	}
	tid := tenantID
	uid := userID
	audit.Detached(h.audit, audit.Entry{
		OccurredAt: time.Now(),
		TenantID:   &tid, UserID: &uid,
		Action:   action,
		Target:   target,
		Metadata: meta,
	}, nil)
}

// redact replaces AuthToken in API responses with a fixed placeholder. The
// raw token is never exposed to clients after creation (handler never reads
// the redacted value, and the DB still has the real one).
func redact(s *Server) *Server {
	if s == nil {
		return nil
	}
	copy := *s
	if copy.AuthToken != "" {
		copy.AuthToken = "***"
	}
	return &copy
}

func parseID(c *gin.Context) (uuid.UUID, bool) {
	raw := c.Param("id")
	id, err := uuid.Parse(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return uuid.Nil, false
	}
	return id, true
}

func tryParseID(c *gin.Context) (uuid.UUID, bool) {
	raw := c.Param("id")
	if raw == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func changedFields(req updateReq) []string {
	out := []string{}
	if req.Name != nil {
		out = append(out, "name")
	}
	if req.Description != nil {
		out = append(out, "description")
	}
	if req.URL != nil {
		out = append(out, "url")
	}
	if req.AuthType != nil {
		out = append(out, "auth_type")
	}
	if req.AuthToken != nil {
		out = append(out, "auth_token")
	}
	if req.Headers != nil {
		out = append(out, "headers")
	}
	if req.Enabled != nil {
		out = append(out, "enabled")
	}
	return out
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

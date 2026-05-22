package reflection

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
)

// AdminHandler exposes admin REST under /admin/memory-proposals. The router
// group must already apply auth.Middleware + auth.RequireAdmin before mount.
type AdminHandler struct {
	repo   *Repo
	memSvc MemoryCreator
	audit  audit.Sink
}

// NewAdminHandler builds the handler. memSvc is called on approve to create
// (or dedup into) the matching memory row.
func NewAdminHandler(repo *Repo, memSvc MemoryCreator) *AdminHandler {
	return &AdminHandler{repo: repo, memSvc: memSvc}
}

// WithAuditSink wires the sink for memory.proposal.* entries. Optional.
func (h *AdminHandler) WithAuditSink(s audit.Sink) *AdminHandler {
	if h != nil {
		h.audit = s
	}
	return h
}

func (h *AdminHandler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/admin/memory-proposals")
	g.GET("", h.list)
	g.GET("/:id", h.get)
	g.POST("/:id/approve", h.approve)
	g.POST("/:id/reject", h.reject)
}

type approveReq struct {
	Type    *string  `json:"type,omitempty"`
	Content *string  `json:"content,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

type rejectReq struct {
	Reason string `json:"reason,omitempty"`
}

func (h *AdminHandler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return cl, true
}

func (h *AdminHandler) list(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	f := ListFilter{Status: c.Query("status")}
	if f.Status != "" && !IsValidStatus(f.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_status"})
		return
	}
	if s := c.Query("owner_user_id"); s != "" {
		uid, err := uuid.Parse(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_owner_user_id"})
			return
		}
		f.OwnerUserID = &uid
	}
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_limit"})
			return
		}
		f.Limit = n
	}
	if s := c.Query("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_offset"})
			return
		}
		f.Offset = n
	}
	rows, err := h.repo.ListByTenant(c.Request.Context(), cl.TenantID, f)
	if err != nil {
		if errors.Is(err, ErrInvalidStatus) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_status"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"proposals": rows})
}

func (h *AdminHandler) get(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	row, err := h.repo.Get(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		if errors.Is(err, ErrProposalNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *AdminHandler) approve(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	var req approveReq
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
			return
		}
	}
	p, err := h.repo.Get(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		if errors.Is(err, ErrProposalNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	if p.Status != StatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "already_decided", "status": p.Status})
		return
	}

	typ := p.Type
	if req.Type != nil {
		typ = *req.Type
		if !IsValidType(typ) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_type"})
			return
		}
	}
	content := p.Content
	if req.Content != nil {
		content = *req.Content
		if content == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "empty_content"})
			return
		}
	}
	tags := p.Tags
	if req.Tags != nil {
		tags = req.Tags
	}

	memID, dedupHit, err := h.memSvc.CreateForReflection(c.Request.Context(),
		cl.TenantID, p.OwnerUserID, typ, content, tags, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memory_create_failed", "detail": err.Error()})
		return
	}

	decidedBy := cl.UserID
	stored, err := h.repo.MarkDecided(c.Request.Context(), cl.TenantID, id, StatusApproved, &memID, &decidedBy)
	if err != nil {
		if errors.Is(err, ErrProposalNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		if errors.Is(err, ErrNotPending) {
			c.JSON(http.StatusConflict, gin.H{"error": "already_decided"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}

	h.bumpOutcome(c, "approved")
	h.auditDecision(cl, "memory.proposal.approve", stored.ID.String(), map[string]any{
		"memory_id":         memID.String(),
		"dedup_hit":         dedupHit,
		"by":                "admin",
		"override_type":     req.Type != nil,
		"override_content":  req.Content != nil,
		"override_tags":     req.Tags != nil,
	})
	c.JSON(http.StatusOK, stored)
}

func (h *AdminHandler) reject(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	var req rejectReq
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
			return
		}
	}
	decidedBy := cl.UserID
	stored, err := h.repo.MarkDecided(c.Request.Context(), cl.TenantID, id, StatusRejected, nil, &decidedBy)
	if err != nil {
		if errors.Is(err, ErrProposalNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		if errors.Is(err, ErrNotPending) {
			c.JSON(http.StatusConflict, gin.H{"error": "already_decided"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	h.bumpOutcome(c, "rejected")
	meta := map[string]any{"by": "admin"}
	if req.Reason != "" {
		meta["reason"] = req.Reason
	}
	h.auditDecision(cl, "memory.proposal.reject", stored.ID.String(), meta)
	c.JSON(http.StatusOK, stored)
}

func (h *AdminHandler) bumpOutcome(c *gin.Context, outcome string) {
	if pcametrics.ReflectionProposalsTotal == nil {
		return
	}
	pcametrics.ReflectionProposalsTotal.Add(c.Request.Context(), 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
	))
}

func (h *AdminHandler) auditDecision(cl *auth.Claims, action, target string, meta map[string]any) {
	if h.audit == nil || cl == nil {
		return
	}
	tid := cl.TenantID
	uid := cl.UserID
	audit.Detached(h.audit, audit.Entry{
		TenantID: &tid, UserID: &uid,
		Action: action, Target: target,
		Metadata: meta,
	}, nil)
}

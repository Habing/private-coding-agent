package memory

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
)

// ReEmbedService is the subset of *Service the admin handler needs.
type ReEmbedService interface {
	ReEmbedTenant(ctx context.Context, tenantID uuid.UUID) (*ReEmbedResult, error)
}

// AdminHandler exposes tenant admin memory maintenance routes.
type AdminHandler struct {
	svc   ReEmbedService
	audit audit.Sink
}

func NewAdminHandler(svc ReEmbedService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

func (h *AdminHandler) WithAuditSink(s audit.Sink) *AdminHandler {
	h.audit = s
	return h
}

func (h *AdminHandler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/admin/memories")
	g.POST("/re-embed", h.reEmbed)
}

func (h *AdminHandler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return cl, true
}

func (h *AdminHandler) reEmbed(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	start := time.Now()
	h.auditEvent(cl, "memory.reembed.start", "tenant", http.StatusAccepted, map[string]any{
		"embedding_model": "",
	})

	res, err := h.svc.ReEmbedTenant(c.Request.Context(), cl.TenantID)
	if err != nil {
		switch {
		case errors.Is(err, ErrReEmbedDisabled):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "reembed_disabled"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		}
		return
	}

	h.auditEvent(cl, "memory.reembed.complete", "tenant", http.StatusOK, map[string]any{
		"total":           res.Total,
		"updated":         res.Updated,
		"failed":          res.Failed,
		"embedding_model": res.EmbeddingModel,
		"duration_ms":     time.Since(start).Milliseconds(),
	})
	c.JSON(http.StatusOK, res)
}

func (h *AdminHandler) auditEvent(cl *auth.Claims, action, target string, status int, meta map[string]any) {
	if h.audit == nil {
		return
	}
	tid, uid := cl.TenantID, cl.UserID
	audit.Detached(h.audit, audit.Entry{
		OccurredAt: time.Now(),
		TenantID:   &tid, UserID: &uid,
		Action: action, Target: target,
		Status: status, Metadata: meta,
	}, nil)
}

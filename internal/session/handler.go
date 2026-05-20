package session

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// HandlerService is the subset of *Service consumed by the REST handler.
// Declared locally so handler_test.go can supply a mock without standing up
// repos / engine.
type HandlerService interface {
	CreateSession(ctx context.Context, tenantID, userID uuid.UUID, req CreateRequest) (*Session, error)
	ListSessions(ctx context.Context, tenantID, userID uuid.UUID) ([]Session, error)
	GetSession(ctx context.Context, tenantID, userID, id uuid.UUID) (*Session, error)
	ArchiveSession(ctx context.Context, tenantID, userID, id uuid.UUID) error
	ListMessages(ctx context.Context, tenantID, userID, sid uuid.UUID) ([]Message, error)
}

// Handler exposes /sessions/* REST endpoints. It is mounted by NewHandler.Register
// onto a gin.RouterGroup that has already been wrapped with auth.Middleware.
type Handler struct {
	svc HandlerService
}

func NewHandler(svc HandlerService) *Handler { return &Handler{svc: svc} }

// Register mounts the REST routes. The WebSocket route is registered separately
// by WSHandler.Register so the two concerns stay independent.
func (h *Handler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/sessions")
	g.POST("", h.create)
	g.GET("", h.list)
	g.GET("/:id", h.get)
	g.DELETE("/:id", h.archive)
	g.GET("/:id/messages", h.listMessages)
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
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: id"})
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) create(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req CreateRequest
	_ = c.ShouldBindJSON(&req) // body optional fields; model required is validated by service
	sess, err := h.svc.CreateSession(c.Request.Context(), cl.TenantID, cl.UserID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrModelRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "model_required"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		}
		return
	}
	c.JSON(http.StatusCreated, sess)
}

type listResp struct {
	Sessions []Session `json:"sessions"`
}

func (h *Handler) list(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	rows, err := h.svc.ListSessions(c.Request.Context(), cl.TenantID, cl.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, listResp{Sessions: rows})
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
	sess, err := h.svc.GetSession(c.Request.Context(), cl.TenantID, cl.UserID, id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (h *Handler) archive(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if err := h.svc.ArchiveSession(c.Request.Context(), cl.TenantID, cl.UserID, id); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.Status(http.StatusNoContent)
}

type messagesResp struct {
	Messages []Message `json:"messages"`
}

func (h *Handler) listMessages(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	msgs, err := h.svc.ListMessages(c.Request.Context(), cl.TenantID, cl.UserID, id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, messagesResp{Messages: msgs})
}

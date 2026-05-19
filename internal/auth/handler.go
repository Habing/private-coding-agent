package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

// AuthService authenticates a (tenant, email, password) triple and returns the
// resolved user. It is satisfied by *user.Service in production and by fakes in
// tests.
type AuthService interface {
	Authenticate(ctx context.Context, tenantID uuid.UUID, email, password string) (*user.User, error)
}

// TenantLookup resolves a tenant slug to its ID. It is the slim contract the
// auth handler needs from the tenant package, satisfied by *tenant.Lookup.
type TenantLookup interface {
	GetBySlug(ctx context.Context, slug string) (uuid.UUID, error)
}

// HandlerDeps bundles the collaborators required by the auth HTTP handler.
type HandlerDeps struct {
	Tenants TenantLookup
	Auth    AuthService
	JWT     *JWT
}

// Handler exposes the /auth/* HTTP endpoints.
type Handler struct{ d HandlerDeps }

// NewHandler returns a Handler configured with the given dependencies.
func NewHandler(d HandlerDeps) *Handler { return &Handler{d: d} }

type loginReq struct {
	Tenant   string `json:"tenant" binding:"required"`
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginResp struct {
	Token string `json:"token"`
}

// Register mounts the auth routes on the given engine.
func (h *Handler) Register(r *gin.Engine) {
	r.POST("/auth/login", h.login)
}

func (h *Handler) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	tid, err := h.d.Tenants.GetBySlug(c.Request.Context(), req.Tenant)
	if err != nil {
		if errors.Is(err, tenant.ErrNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "bad_credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	u, err := h.d.Auth.Authenticate(c.Request.Context(), tid, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, user.ErrBadCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "bad_credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	tok, err := h.d.JWT.Issue(u.ID, u.TenantID, string(u.Role))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, loginResp{Token: tok})
}

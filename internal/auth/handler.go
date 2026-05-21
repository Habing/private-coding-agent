package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
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
	// OIDCUsers resolves IdP identities (JIT). Required when OIDC is enabled.
	OIDCUsers OIDCUserService
	JWT       *JWT
	// LocalEnabled gates POST /auth/login (default true).
	LocalEnabled bool
	// OIDC is optional; when nil or Config.Enabled=false the OIDC routes are off.
	OIDC *OIDCRuntime
	// Audit is optional; when non-nil the handler appends auth.login.success /
	// auth.login.failure entries for every login attempt. nil = no audit.
	Audit audit.Sink
	// Revoker is optional; when non-nil, POST /auth/logout records the
	// caller's jti so subsequent requests with the same token are rejected.
	// nil disables /auth/logout (it returns 501 Not Implemented).
	Revoker Revoker
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
	if h.d.LocalEnabled {
		r.POST("/auth/login", h.login)
	}
	r.POST("/auth/logout", h.logout)
	if h.oidcEnabled() {
		r.GET("/auth/oidc/login", h.oidcLogin)
		r.GET("/auth/oidc/callback", h.oidcCallback)
	}
}

func (h *Handler) login(c *gin.Context) {
	start := time.Now()
	if !h.d.LocalEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "local_login_disabled"})
		return
	}
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	tid, err := h.d.Tenants.GetBySlug(c.Request.Context(), req.Tenant)
	if err != nil {
		if errors.Is(err, tenant.ErrNotFound) {
			h.auditLoginFailure(c, start, nil, req, "wrong_tenant")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "bad_credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	u, err := h.d.Auth.Authenticate(c.Request.Context(), tid, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, user.ErrBadCredentials) {
			h.auditLoginFailure(c, start, &tid, req, "bad_credentials")
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
	h.auditLoginSuccess(c, start, u, req)
	c.JSON(http.StatusOK, loginResp{Token: tok})
}

// logout revokes the bearer token in the Authorization header. We parse the
// token ourselves (rather than running behind auth.Middleware) so this
// endpoint can be mounted on the public router group — convenient for
// clients that want to log out even when their cached token is on the
// verge of expiry. We still require the token to be syntactically valid;
// expired tokens get a 200 (idempotent) since revocation is moot.
func (h *Handler) logout(c *gin.Context) {
	if h.d.Revoker == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "revocation_disabled"})
		return
	}
	hdr := c.GetHeader("Authorization")
	if !strings.HasPrefix(hdr, "Bearer ") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing_token"})
		return
	}
	tok := strings.TrimPrefix(hdr, "Bearer ")
	cl, err := h.d.JWT.Parse(tok)
	if err != nil {
		// Treat expired/invalid as a successful idempotent logout: the token
		// is already useless, so the user-visible state is "logged out."
		c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
		return
	}
	ttl := time.Until(cl.ExpiresAt)
	if err := h.d.Revoker.Revoke(c.Request.Context(), cl.JTI, ttl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "revocation_failed"})
		return
	}
	if h.d.Audit != nil {
		tid := cl.TenantID
		uid := cl.UserID
		audit.Detached(h.d.Audit, audit.Entry{
			OccurredAt: time.Now(),
			TenantID:   &tid, UserID: &uid,
			Action: "auth.logout",
			Target: cl.JTI,
			Method: c.Request.Method, Path: c.FullPath(),
			Status: http.StatusOK,
		}, nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
}

func (h *Handler) auditLoginSuccess(c *gin.Context, start time.Time, u *user.User, req loginReq) {
	if h.d.Audit == nil {
		return
	}
	tid := u.TenantID
	uid := u.ID
	audit.Detached(h.d.Audit, audit.Entry{
		OccurredAt: start,
		TenantID:   &tid, UserID: &uid,
		Action: "auth.login.success",
		Target: req.Email,
		Method: c.Request.Method, Path: c.FullPath(),
		Status:     http.StatusOK,
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata: map[string]any{
			"tenant_slug": req.Tenant,
			"role":        string(u.Role),
		},
	}, nil)
}

func (h *Handler) auditLoginFailure(c *gin.Context, start time.Time, tenantID *uuid.UUID, req loginReq, reason string) {
	if h.d.Audit == nil {
		return
	}
	audit.Detached(h.d.Audit, audit.Entry{
		OccurredAt: start,
		TenantID:   tenantID,
		Action:     "auth.login.failure",
		Target:     req.Email,
		Method:     c.Request.Method, Path: c.FullPath(),
		Status:     http.StatusUnauthorized,
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata: map[string]any{
			"tenant_slug": req.Tenant,
			"reason":      reason,
		},
	}, nil)
}

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

// OIDCUserService maps IdP identities to local users (JIT).
type OIDCUserService interface {
	FindOrCreateOIDC(ctx context.Context, tenantID uuid.UUID, iss, sub, email, name string) (*user.User, error)
}

// OIDCRuntime bundles a configured OIDC client and cookie signing secret.
type OIDCRuntime struct {
	Config       OIDCConfig
	Client       *OIDCClient
	CookieSecret string
}

// HandlerDeps gains OIDC + LocalEnabled (see handler.go).

func (h *Handler) oidcEnabled() bool {
	return h.d.OIDC != nil && h.d.OIDC.Config.Enabled && h.d.OIDC.Client != nil
}

func (h *Handler) oidcLogin(c *gin.Context) {
	start := time.Now()
	if !h.oidcEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "oidc_disabled"})
		return
	}
	tenantSlug := c.Query("tenant")
	if tenantSlug == "" {
		tenantSlug = h.d.OIDC.Config.TenantSlug
	}
	state, err := randomURLSafe(24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	nonce, err := randomURLSafe(24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	authURL, verifier, err := h.d.OIDC.Client.AuthCodeURL(state, nonce)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	flow, err := newOIDCFlow(tenantSlug, state, nonce, verifier, 10*time.Minute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	signed, err := signOIDCData(h.d.OIDC.CookieSecret, flow)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.SetCookie(oidcCookieName, signed, 600, "/", "", false, true)
	c.Redirect(http.StatusFound, authURL)
	_ = start
}

func (h *Handler) oidcCallback(c *gin.Context) {
	start := time.Now()
	if !h.oidcEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "oidc_disabled"})
		return
	}
	if errMsg := c.Query("error"); errMsg != "" {
		h.auditOIDCFailure(c, start, nil, "idp_error:"+errMsg)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "oidc_denied"})
		return
	}
	rawCookie, err := c.Cookie(oidcCookieName)
	if err != nil {
		h.auditOIDCFailure(c, start, nil, "missing_cookie")
		c.JSON(http.StatusBadRequest, gin.H{"error": "oidc_state_missing"})
		return
	}
	flow, err := verifyOIDCData(h.d.OIDC.CookieSecret, rawCookie)
	if err != nil {
		h.auditOIDCFailure(c, start, nil, "bad_cookie")
		c.JSON(http.StatusBadRequest, gin.H{"error": "oidc_state_invalid"})
		return
	}
	c.SetCookie(oidcCookieName, "", -1, "/", "", false, true)
	if c.Query("state") != flow.State {
		h.auditOIDCFailure(c, start, nil, "state_mismatch")
		c.JSON(http.StatusBadRequest, gin.H{"error": "oidc_state_mismatch"})
		return
	}
	claims, err := h.d.OIDC.Client.ExchangeCode(c.Request.Context(), c.Query("code"), flow.CodeVerifier)
	if err != nil {
		h.auditOIDCFailure(c, start, nil, "token_exchange")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "oidc_exchange_failed"})
		return
	}
	if claims.Nonce != flow.Nonce {
		h.auditOIDCFailure(c, start, nil, "nonce_mismatch")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "oidc_nonce_mismatch"})
		return
	}
	tid, err := h.d.Tenants.GetBySlug(c.Request.Context(), flow.TenantSlug)
	if err != nil {
		if errors.Is(err, tenant.ErrNotFound) {
			h.auditOIDCFailure(c, start, nil, "wrong_tenant")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "oidc_tenant_invalid"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	if h.d.OIDCUsers == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	u, err := h.d.OIDCUsers.FindOrCreateOIDC(c.Request.Context(), tid, claims.Iss, claims.Sub, claims.Email, claims.Name)
	if err != nil {
		h.auditOIDCFailure(c, start, &tid, "jit_failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	tok, err := h.d.JWT.Issue(u.ID, u.TenantID, string(u.Role))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	h.auditOIDCSuccess(c, start, u, flow.TenantSlug)
	c.JSON(http.StatusOK, loginResp{Token: tok})
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(b)
	if len(s) < n {
		return s, nil
	}
	return s[:n], nil
}

func (h *Handler) auditOIDCSuccess(c *gin.Context, start time.Time, u *user.User, tenantSlug string) {
	if h.d.Audit == nil {
		return
	}
	tid := u.TenantID
	uid := u.ID
	audit.Detached(h.d.Audit, audit.Entry{
		OccurredAt: start,
		TenantID:   &tid, UserID: &uid,
		Action: "auth.oidc.login.success",
		Target: u.Email,
		Method: c.Request.Method, Path: c.FullPath(),
		Status:     http.StatusOK,
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata: map[string]any{
			"tenant_slug": tenantSlug,
			"role":        string(u.Role),
			"oidc_iss":    u.OIDCIss,
			"oidc_sub":    u.OIDCSub,
		},
	}, nil)
}

func (h *Handler) auditOIDCFailure(c *gin.Context, start time.Time, tenantID *uuid.UUID, reason string) {
	if h.d.Audit == nil {
		return
	}
	audit.Detached(h.d.Audit, audit.Entry{
		OccurredAt: start,
		TenantID:   tenantID,
		Action:     "auth.oidc.login.failure",
		Target:     c.Query("state"),
		Method:     c.Request.Method, Path: c.FullPath(),
		Status:     http.StatusUnauthorized,
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata:   map[string]any{"reason": reason},
	}, nil)
}

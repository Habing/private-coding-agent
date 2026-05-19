package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func newProtectedRouter(t *testing.T, secret string) (*gin.Engine, *auth.JWT) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	r := gin.New()
	r.Use(auth.Middleware(j))
	r.GET("/me", func(c *gin.Context) {
		cl := auth.FromCtx(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"uid": cl.UserID, "tid": cl.TenantID, "role": cl.Role})
	})
	return r, j
}

func TestMiddleware_OK(t *testing.T) {
	r, j := newProtectedRouter(t, "test-secret-thirty-two-chars-ok!")
	uid, tid := uuid.New(), uuid.New()
	tok, _ := j.Issue(uid, tid, "member")

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), uid.String())
}

func TestMiddleware_MissingHeader(t *testing.T) {
	r, _ := newProtectedRouter(t, "test-secret-thirty-two-chars-ok!")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_BadToken(t *testing.T) {
	r, _ := newProtectedRouter(t, "test-secret-thirty-two-chars-ok!")
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

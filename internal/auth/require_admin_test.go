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

func newAdminRouter(t *testing.T) (*gin.Engine, *auth.JWT) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	j := auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour})
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	g.Use(auth.RequireAdmin())
	g.GET("/admin/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r, j
}

func TestRequireAdmin_AdminTokenPasses(t *testing.T) {
	r, j := newAdminRouter(t)
	tok, _ := j.Issue(uuid.New(), uuid.New(), "admin")
	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"ok":true`)
}

func TestRequireAdmin_MemberTokenRejected(t *testing.T) {
	r, j := newAdminRouter(t)
	tok, _ := j.Issue(uuid.New(), uuid.New(), "member")
	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "forbidden")
}

func TestRequireAdmin_EmptyRoleRejected(t *testing.T) {
	r, j := newAdminRouter(t)
	tok, _ := j.Issue(uuid.New(), uuid.New(), "")
	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdmin_NoMiddlewareIsRejected(t *testing.T) {
	// Mounting RequireAdmin without Middleware: there are no Claims in ctx, so
	// every request must be rejected with 403.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.RequireAdmin())
	g.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
}

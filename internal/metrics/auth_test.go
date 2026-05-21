package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
)

const staticToken = "static-scrape-token"

func setupAuthRouter(t *testing.T) (*gin.Engine, *auth.JWT) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	jwt := auth.NewJWT(auth.JWTConfig{
		Secret: "test-secret-of-sufficient-length-1234",
		TTL:    time.Hour,
	})
	r := gin.New()
	r.GET("/metrics",
		pcametrics.Auth(pcametrics.AuthConfig{JWT: jwt, StaticToken: staticToken}),
		func(c *gin.Context) { c.String(http.StatusOK, "ok") },
	)
	return r, jwt
}

func TestMetricsAuth_StaticTokenPasses(t *testing.T) {
	r, _ := setupAuthRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+staticToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestMetricsAuth_AdminJWTPasses(t *testing.T) {
	r, jwt := setupAuthRouter(t)

	tok, err := jwt.Issue(uuid.New(), uuid.New(), auth.RoleAdmin)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestMetricsAuth_MemberJWTRejected(t *testing.T) {
	r, jwt := setupAuthRouter(t)

	tok, err := jwt.Issue(uuid.New(), uuid.New(), "member")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestMetricsAuth_MissingHeaderRejected(t *testing.T) {
	r, _ := setupAuthRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMetricsAuth_BadTokenRejected(t *testing.T) {
	r, _ := setupAuthRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

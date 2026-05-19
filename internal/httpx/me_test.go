package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/httpx"
)

func TestRegisterMe_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	j := auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, err := j.Issue(uid, tid, "admin")
	require.NoError(t, err)

	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	httpx.RegisterMe(g)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.True(t, strings.Contains(body, uid.String()))
	require.True(t, strings.Contains(body, tid.String()))
	require.True(t, strings.Contains(body, "admin"))
}

func TestRegisterMe_Unauthorized_WithoutToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 故意不挂 Middleware
	r := gin.New()
	g := r.Group("/")
	httpx.RegisterMe(g)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

package memory_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/memory"
)

type mockReEmbed struct {
	res *memory.ReEmbedResult
	err error
}

func (m *mockReEmbed) ReEmbedTenant(context.Context, uuid.UUID) (*memory.ReEmbedResult, error) {
	return m.res, m.err
}

func newAdminRouter(t *testing.T, svc memory.ReEmbedService, role string) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, err := j.Issue(uid, tid, role)
	require.NoError(t, err)
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	g.Use(auth.RequireAdmin())
	memory.NewAdminHandler(svc).Register(g)
	return r, "Bearer " + tok
}

func TestAdminHandler_ReEmbed_OK(t *testing.T) {
	svc := &mockReEmbed{res: &memory.ReEmbedResult{Total: 2, Updated: 2, EmbeddingModel: "mock:emb"}}
	r, tok := newAdminRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodPost, "/admin/memories/re-embed", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"updated":2`)
}

func TestAdminHandler_ReEmbed_Disabled(t *testing.T) {
	svc := &mockReEmbed{err: memory.ErrReEmbedDisabled}
	r, tok := newAdminRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodPost, "/admin/memories/re-embed", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestAdminHandler_ReEmbed_RequiresAdmin(t *testing.T) {
	svc := &mockReEmbed{res: &memory.ReEmbedResult{}}
	r, tok := newAdminRouter(t, svc, "member")
	req := httptest.NewRequest(http.MethodPost, "/admin/memories/re-embed", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminHandler_ReEmbed_Internal(t *testing.T) {
	svc := &mockReEmbed{err: errors.New("boom")}
	r, tok := newAdminRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodPost, "/admin/memories/re-embed", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

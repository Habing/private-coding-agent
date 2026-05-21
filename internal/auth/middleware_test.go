package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// fakeRevoker is a thread-safe in-memory Revoker for middleware tests.
type fakeRevoker struct {
	mu       sync.Mutex
	revoked  map[string]bool
	wantErr  error
}

func (f *fakeRevoker) IsRevoked(_ context.Context, jti string) (bool, error) {
	if f.wantErr != nil {
		return false, f.wantErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoked[jti], nil
}

func (f *fakeRevoker) Revoke(_ context.Context, jti string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revoked == nil {
		f.revoked = map[string]bool{}
	}
	f.revoked[jti] = true
	return nil
}

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

func newRouterWithRevoker(t *testing.T, secret string, rev auth.Revoker) (*gin.Engine, *auth.JWT) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	r := gin.New()
	r.Use(auth.Middleware(j, auth.WithRevoker(rev)))
	r.GET("/me", func(c *gin.Context) {
		cl := auth.FromCtx(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"uid": cl.UserID, "jti": cl.JTI})
	})
	return r, j
}

func TestMiddleware_RevokedToken(t *testing.T) {
	rev := &fakeRevoker{}
	r, j := newRouterWithRevoker(t, "test-secret-thirty-two-chars-ok!", rev)
	tok, _ := j.Issue(uuid.New(), uuid.New(), "member")

	// Pull jti out by parsing the token.
	cl, err := j.Parse(tok)
	require.NoError(t, err)
	require.NotEmpty(t, cl.JTI)

	// Token works before revocation.
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Revoke, then the same token is refused.
	_ = rev.Revoke(context.Background(), cl.JTI, time.Hour)
	req2 := httptest.NewRequest(http.MethodGet, "/me", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusUnauthorized, w2.Code)
	require.Contains(t, w2.Body.String(), "token_revoked")
}

func TestMiddleware_RevokerStoreError(t *testing.T) {
	rev := &fakeRevoker{wantErr: errors.New("redis down")}
	r, j := newRouterWithRevoker(t, "test-secret-thirty-two-chars-ok!", rev)
	tok, _ := j.Issue(uuid.New(), uuid.New(), "member")
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// Fail-closed when revocation lookup fails — better to reject one
	// session than admit a (potentially) revoked one.
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWT_IssueGeneratesUniqueJTI(t *testing.T) {
	j := auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour})
	tokA, _ := j.Issue(uuid.New(), uuid.New(), "member")
	tokB, _ := j.Issue(uuid.New(), uuid.New(), "member")
	clA, _ := j.Parse(tokA)
	clB, _ := j.Parse(tokB)
	require.NotEmpty(t, clA.JTI)
	require.NotEmpty(t, clB.JTI)
	require.NotEqual(t, clA.JTI, clB.JTI)
	require.False(t, clA.ExpiresAt.IsZero())
}

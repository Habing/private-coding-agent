package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

type spySink struct {
	mu  sync.Mutex
	got []audit.Entry
}

func (s *spySink) Append(_ context.Context, e audit.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	return nil
}

func (s *spySink) entries() []audit.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Entry, len(s.got))
	copy(out, s.got)
	return out
}

func newLoginRouter(t *testing.T, tenants auth.TenantLookup, authSvc auth.AuthService, sink audit.Sink) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	j := auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour})
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: tenants, Auth: authSvc, JWT: j, Audit: sink,
	})
	r := gin.New()
	h.Register(r)
	return r
}

func doLogin(r *gin.Engine, body map[string]string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(raw)))
	return w
}

func TestLogin_Audit_SuccessEntry(t *testing.T) {
	tid := uuid.New()
	u := &user.User{ID: uuid.New(), TenantID: tid, Email: "demo@example.com", Role: user.RoleAdmin}
	sink := &spySink{}
	r := newLoginRouter(t, fakeTenants{id: tid}, fakeAuth{user: u}, sink)
	w := doLogin(r, map[string]string{"tenant": "default", "email": "demo@example.com", "password": "x"})
	require.Equal(t, http.StatusOK, w.Code)

	require.Eventually(t, func() bool { return len(sink.entries()) >= 1 }, time.Second, 10*time.Millisecond)
	e := sink.entries()[0]
	require.Equal(t, "auth.login.success", e.Action)
	require.Equal(t, "demo@example.com", e.Target)
	require.NotNil(t, e.TenantID)
	require.Equal(t, tid, *e.TenantID)
	require.NotNil(t, e.UserID)
	require.Equal(t, u.ID, *e.UserID)
	require.Equal(t, "admin", e.Metadata["role"])
	require.Equal(t, "default", e.Metadata["tenant_slug"])
	require.Equal(t, http.StatusOK, e.Status)
}

func TestLogin_Audit_BadCredentialsFailure(t *testing.T) {
	tid := uuid.New()
	sink := &spySink{}
	r := newLoginRouter(t, fakeTenants{id: tid}, fakeAuth{err: user.ErrBadCredentials}, sink)
	w := doLogin(r, map[string]string{"tenant": "default", "email": "demo@example.com", "password": "wrong"})
	require.Equal(t, http.StatusUnauthorized, w.Code)

	require.Eventually(t, func() bool { return len(sink.entries()) >= 1 }, time.Second, 10*time.Millisecond)
	e := sink.entries()[0]
	require.Equal(t, "auth.login.failure", e.Action)
	require.Equal(t, "demo@example.com", e.Target)
	require.NotNil(t, e.TenantID, "failure entry should still tag tenant when slug resolved")
	require.Equal(t, tid, *e.TenantID)
	require.Equal(t, "bad_credentials", e.Metadata["reason"])
	require.Equal(t, http.StatusUnauthorized, e.Status)
}

func TestLogin_Audit_UnknownTenantFailure(t *testing.T) {
	sink := &spySink{}
	r := newLoginRouter(t, fakeTenants{err: tenant.ErrNotFound}, fakeAuth{}, sink)
	w := doLogin(r, map[string]string{"tenant": "ghost", "email": "demo@example.com", "password": "x"})
	require.Equal(t, http.StatusUnauthorized, w.Code)

	require.Eventually(t, func() bool { return len(sink.entries()) >= 1 }, time.Second, 10*time.Millisecond)
	e := sink.entries()[0]
	require.Equal(t, "auth.login.failure", e.Action)
	require.Nil(t, e.TenantID, "tenant lookup failed -> no tenant ID known")
	require.Equal(t, "wrong_tenant", e.Metadata["reason"])
}

func TestLogin_Audit_NilSinkIsSafe(t *testing.T) {
	tid := uuid.New()
	r := newLoginRouter(t, fakeTenants{id: tid},
		fakeAuth{user: &user.User{ID: uuid.New(), TenantID: tid, Role: user.RoleMember}},
		nil)
	w := doLogin(r, map[string]string{"tenant": "default", "email": "x@example.com", "password": "y"})
	require.Equal(t, http.StatusOK, w.Code)
}

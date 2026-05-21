package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

type fakeAuth struct {
	user *user.User
	err  error
}

func (f fakeAuth) Authenticate(_ context.Context, _ uuid.UUID, _, _ string) (*user.User, error) {
	return f.user, f.err
}

type fakeTenants struct {
	id  uuid.UUID
	err error
}

func (f fakeTenants) GetBySlug(_ context.Context, _ string) (uuid.UUID, error) {
	return f.id, f.err
}

func TestLoginOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tid, uid := uuid.New(), uuid.New()
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{id: tid},
		Auth:    fakeAuth{user: &user.User{ID: uid, TenantID: tid, Role: user.RoleMember}},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)

	body, _ := json.Marshal(map[string]string{
		"tenant": "default", "email": "a@b", "password": "x",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["token"])
}

func TestLogin_BadCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{id: uuid.New()},
		Auth:    fakeAuth{err: user.ErrBadCredentials},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)

	body, _ := json.Marshal(map[string]string{
		"tenant": "default", "email": "a@b", "password": "x",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogin_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{id: uuid.New()},
		Auth:    fakeAuth{err: errors.New("boom")},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)
	body, _ := json.Marshal(map[string]string{
		"tenant": "default", "email": "a@b", "password": "x",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestLogin_TenantNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{err: tenant.ErrNotFound},
		Auth:    fakeAuth{},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)

	body, _ := json.Marshal(map[string]string{
		"tenant": "missing", "email": "a@b", "password": "x",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogin_TenantLookupError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{err: errors.New("db connection refused")},
		Auth:    fakeAuth{},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)

	body, _ := json.Marshal(map[string]string{
		"tenant": "default", "email": "a@b", "password": "x",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestLogin_BindFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{id: uuid.New()},
		Auth:    fakeAuth{},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)

	// missing required fields
	body := []byte(`{"tenant":""}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLogout_Revokes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rev := &fakeRevoker{}
	j := auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour})
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{id: uuid.New()},
		Auth:    fakeAuth{},
		JWT:     j,
		Revoker: rev,
	})
	r := gin.New()
	h.Register(r)

	tok, _ := j.Issue(uuid.New(), uuid.New(), "member")
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	cl, err := j.Parse(tok)
	require.NoError(t, err)
	revoked, _ := rev.IsRevoked(context.Background(), cl.JTI)
	require.True(t, revoked, "logout should mark jti revoked")
}

func TestLogout_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rev := &fakeRevoker{}
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{id: uuid.New()},
		Auth:    fakeAuth{},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour}),
		Revoker: rev,
	})
	r := gin.New()
	h.Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLogout_NoRevoker_Returns501(t *testing.T) {
	gin.SetMode(gin.TestMode)
	j := auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour})
	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants: fakeTenants{id: uuid.New()},
		Auth:    fakeAuth{},
		JWT:     j,
		// Revoker omitted
	})
	r := gin.New()
	h.Register(r)

	tok, _ := j.Issue(uuid.New(), uuid.New(), "member")
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotImplemented, w.Code)
}

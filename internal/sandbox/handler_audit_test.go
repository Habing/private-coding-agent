package sandbox_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

type spySandboxSink struct {
	mu  sync.Mutex
	got []audit.Entry
}

func (s *spySandboxSink) Append(_ context.Context, e audit.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	return nil
}

func (s *spySandboxSink) entries() []audit.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Entry, len(s.got))
	copy(out, s.got)
	return out
}

func newRouterWithAudit(t *testing.T, m *mockRuntime, sink audit.Sink) (*gin.Engine, string, uuid.UUID, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, err := j.Issue(uid, tid, "member")
	require.NoError(t, err)

	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	sandbox.NewHandler(m).WithAuditSink(sink).Register(g)
	return r, "Bearer " + tok, tid, uid
}

func TestHandler_Create_EmitsAudit(t *testing.T) {
	sbID := uuid.New()
	mr := &mockRuntime{
		createRet: &sandbox.Sandbox{
			ID:        sbID,
			Status:    sandbox.StatusRunning,
			Image:     "pca/sandbox:base",
			Network:   sandbox.NetworkInternal,
			Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
		},
	}
	sink := &spySandboxSink{}
	r, tok, _, _ := newRouterWithAudit(t, mr, sink)
	// mockRuntime returns Sandbox with zero tenant/owner IDs; that's fine for
	// audit assertions because we only verify action + target + metadata here.
	w := do(r, http.MethodPost, "/sandbox/sessions", tok, map[string]any{})
	require.Equal(t, http.StatusCreated, w.Code)

	require.Eventually(t, func() bool { return len(sink.entries()) >= 1 }, time.Second, 10*time.Millisecond)
	e := sink.entries()[0]
	require.Equal(t, "sandbox.create", e.Action)
	require.Equal(t, sbID.String(), e.Target)
	require.Equal(t, "pca/sandbox:base", e.Metadata["image"])
	require.Equal(t, http.StatusCreated, e.Status)
}

func TestHandler_Destroy_EmitsAudit(t *testing.T) {
	mr := &mockRuntime{}
	sink := &spySandboxSink{}
	r, tok, tid, uid := newRouterWithAudit(t, mr, sink)
	sbID := uuid.New()
	w := do(r, http.MethodDelete, "/sandbox/sessions/"+sbID.String(), tok, nil)
	require.Equal(t, http.StatusNoContent, w.Code)

	require.Eventually(t, func() bool { return len(sink.entries()) >= 1 }, time.Second, 10*time.Millisecond)
	e := sink.entries()[0]
	require.Equal(t, "sandbox.destroy", e.Action)
	require.Equal(t, sbID.String(), e.Target)
	require.NotNil(t, e.TenantID)
	require.Equal(t, tid, *e.TenantID)
	require.NotNil(t, e.UserID)
	require.Equal(t, uid, *e.UserID)
	require.Equal(t, http.StatusNoContent, e.Status)
}

func TestHandler_Create_Failure_NoAudit(t *testing.T) {
	mr := &mockRuntime{createErr: sandbox.ErrSandboxNotReady}
	sink := &spySandboxSink{}
	r, tok, _, _ := newRouterWithAudit(t, mr, sink)
	_ = do(r, http.MethodPost, "/sandbox/sessions", tok, map[string]any{})
	// brief window for any goroutine — but we expect zero entries
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, sink.entries(), "failed Create must not emit audit entry")
}

func TestHandler_NilSink_Safe(t *testing.T) {
	mr := &mockRuntime{
		createRet: &sandbox.Sandbox{ID: uuid.New(), Status: sandbox.StatusRunning},
	}
	r, tok, _, _ := newRouterWithAudit(t, mr, nil)
	w := do(r, http.MethodPost, "/sandbox/sessions", tok, map[string]any{})
	require.Equal(t, http.StatusCreated, w.Code)
}

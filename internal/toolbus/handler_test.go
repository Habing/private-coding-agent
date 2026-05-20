package toolbus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// mockBus satisfies the small surface used by Handler.
type mockBus struct {
	listRet   []toolbus.ToolDef
	invokeRet json.RawMessage
	invokeErr error
}

func (m *mockBus) ListTools(_ context.Context, _ uuid.UUID) []toolbus.ToolDef {
	return m.listRet
}
func (m *mockBus) Invoke(_ context.Context, _, _ uuid.UUID, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return m.invokeRet, m.invokeErr
}

func newRouter(t *testing.T, mb *mockBus) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, _ := j.Issue(uid, tid, "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	toolbus.NewHandler(mb).Register(g)
	return r, "Bearer " + tok
}

func TestHandler_List_OK(t *testing.T) {
	mb := &mockBus{listRet: []toolbus.ToolDef{
		{Name: "fs.read", Description: "x", Parameters: json.RawMessage(`{}`)},
	}}
	r, tok := newRouter(t, mb)
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"fs.read"`)
}

func TestHandler_Invoke_OK(t *testing.T) {
	mb := &mockBus{invokeRet: json.RawMessage(`{"ok":true}`)}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{
		"tool":  "fs.read",
		"input": map[string]string{"sandbox_id": uuid.NewString(), "path": "x"},
	})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"ok":true`)
}

func TestHandler_Invoke_NoAuth(t *testing.T) {
	mb := &mockBus{}
	r, _ := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Invoke_ToolMissing(t *testing.T) {
	mb := &mockBus{}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "tool_required")
}

func TestHandler_Invoke_ToolNotFound(t *testing.T) {
	mb := &mockBus{invokeErr: toolbus.ErrToolNotFound}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Invoke_InvalidArgs(t *testing.T) {
	mb := &mockBus{invokeErr: toolbus.ErrInvalidArguments}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Invoke_SandboxNotFound(t *testing.T) {
	mb := &mockBus{invokeErr: sandbox.ErrSandboxNotFound}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Invoke_ProviderUnreachable(t *testing.T) {
	mb := &mockBus{invokeErr: modelgw.ErrProviderUnreachable}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadGateway, w.Code)
}

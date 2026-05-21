package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/session"
)

// mockWSSvc implements WSSendService. SendMessage replays scripted events and
// returns scripted error; recordings let assertions inspect what was called.
type mockWSSvc struct {
	mu sync.Mutex

	getSessFn func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*session.Session, error)
	sendFn    func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, func(agent.Event) error) error

	gotContent string
	cancelled  bool
}

func (m *mockWSSvc) GetSession(ctx context.Context, tid, uid, id uuid.UUID) (*session.Session, error) {
	return m.getSessFn(ctx, tid, uid, id)
}

func (m *mockWSSvc) SendMessage(ctx context.Context, tid, uid, sid uuid.UUID,
	content string, onEvent func(agent.Event) error) error {
	m.mu.Lock()
	m.gotContent = content
	m.mu.Unlock()
	return m.sendFn(ctx, tid, uid, sid, content, onEvent)
}

func newWSServer(t *testing.T, svc session.WSSendService) (string, string, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	tid, uid := uuid.New(), uuid.New()
	tok, _ := j.Issue(uid, tid, "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	session.NewWSHandler(svc, []string{"*"}).Register(g)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL, tok, uuid.New()
}

func dialWS(t *testing.T, baseURL, sid, tok string) *websocket.Conn {
	t.Helper()
	u, _ := url.Parse(baseURL)
	u.Scheme = "ws"
	u.Path = "/sessions/" + sid + "/ws"
	h := http.Header{}
	h.Set("Authorization", "Bearer "+tok)
	c, resp, err := websocket.DefaultDialer.Dial(u.String(), h)
	require.NoErrorf(t, err, "dial: %v (status=%v)", err, resp)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestWS_HappyPath_UserMessageRoundTrip(t *testing.T) {
	sid := uuid.New()
	svc := &mockWSSvc{
		getSessFn: func(_ context.Context, tid, uid, id uuid.UUID) (*session.Session, error) {
			return &session.Session{ID: sid, TenantID: tid, OwnerUserID: uid, Status: session.StatusActive}, nil
		},
		sendFn: func(_ context.Context, _, _, _ uuid.UUID, content string, on func(agent.Event) error) error {
			require.Equal(t, "hello", content)
			require.NoError(t, on(agent.Event{Kind: agent.EventAssistantMessage, Step: 1, Text: "hi back"}))
			require.NoError(t, on(agent.Event{Kind: agent.EventFinal, Step: 1, Text: "hi back", FinishReason: "stop"}))
			return nil
		},
	}
	base, tok, _ := newWSServer(t, svc)
	c := dialWS(t, base, sid.String(), tok)

	require.NoError(t, c.WriteJSON(map[string]string{"type": "user_message", "content": "hello"}))
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))

	var first map[string]json.RawMessage
	require.NoError(t, c.ReadJSON(&first))
	require.Equal(t, `"event"`, string(first["type"]))
	require.Contains(t, string(first["event"]), `"assistant_message"`)

	var second map[string]json.RawMessage
	require.NoError(t, c.ReadJSON(&second))
	require.Contains(t, string(second["event"]), `"final"`)

	var done map[string]string
	require.NoError(t, c.ReadJSON(&done))
	require.Equal(t, "done", done["type"])
}

func TestWS_PingPong(t *testing.T) {
	sid := uuid.New()
	svc := &mockWSSvc{
		getSessFn: func(_ context.Context, tid, uid, _ uuid.UUID) (*session.Session, error) {
			return &session.Session{ID: sid, TenantID: tid, OwnerUserID: uid, Status: session.StatusActive}, nil
		},
	}
	base, tok, _ := newWSServer(t, svc)
	c := dialWS(t, base, sid.String(), tok)

	require.NoError(t, c.WriteJSON(map[string]string{"type": "ping"}))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var got map[string]string
	require.NoError(t, c.ReadJSON(&got))
	require.Equal(t, "pong", got["type"])
}

func TestWS_NotFoundBeforeUpgrade(t *testing.T) {
	svc := &mockWSSvc{
		getSessFn: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*session.Session, error) {
			return nil, session.ErrSessionNotFound
		},
	}
	base, tok, _ := newWSServer(t, svc)
	u, _ := url.Parse(base)
	u.Scheme = "ws"
	u.Path = "/sessions/" + uuid.New().String() + "/ws"
	h := http.Header{}
	h.Set("Authorization", "Bearer "+tok)
	_, resp, err := websocket.DefaultDialer.Dial(u.String(), h)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestWS_Unauthorized(t *testing.T) {
	svc := &mockWSSvc{
		getSessFn: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*session.Session, error) {
			return &session.Session{Status: session.StatusActive}, nil
		},
	}
	base, _, _ := newWSServer(t, svc)
	u, _ := url.Parse(base)
	u.Scheme = "ws"
	u.Path = "/sessions/" + uuid.New().String() + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestWS_EngineError_WritesErrorFrame(t *testing.T) {
	sid := uuid.New()
	svc := &mockWSSvc{
		getSessFn: func(_ context.Context, tid, uid, _ uuid.UUID) (*session.Session, error) {
			return &session.Session{ID: sid, TenantID: tid, OwnerUserID: uid, Status: session.StatusActive}, nil
		},
		sendFn: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, func(agent.Event) error) error {
			return errors.New("boom")
		},
	}
	base, tok, _ := newWSServer(t, svc)
	c := dialWS(t, base, sid.String(), tok)

	require.NoError(t, c.WriteJSON(map[string]string{"type": "user_message", "content": "x"}))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var got map[string]string
	require.NoError(t, c.ReadJSON(&got))
	require.Equal(t, "error", got["type"])
	require.Equal(t, "boom", got["message"])
}

func TestWS_Archived_WritesErrorFrame(t *testing.T) {
	sid := uuid.New()
	svc := &mockWSSvc{
		getSessFn: func(_ context.Context, tid, uid, _ uuid.UUID) (*session.Session, error) {
			return &session.Session{ID: sid, TenantID: tid, OwnerUserID: uid, Status: session.StatusActive}, nil
		},
		sendFn: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, func(agent.Event) error) error {
			return session.ErrSessionArchived
		},
	}
	base, tok, _ := newWSServer(t, svc)
	c := dialWS(t, base, sid.String(), tok)
	require.NoError(t, c.WriteJSON(map[string]string{"type": "user_message", "content": "x"}))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var got map[string]string
	require.NoError(t, c.ReadJSON(&got))
	require.Equal(t, "error", got["type"])
	require.Equal(t, "archived", got["code"])
}

func TestWS_ClientDisconnect_RunStillCompletes(t *testing.T) {
	sid := uuid.New()
	done := make(chan struct{}, 1)
	svc := &mockWSSvc{
		getSessFn: func(_ context.Context, tid, uid, _ uuid.UUID) (*session.Session, error) {
			return &session.Session{ID: sid, TenantID: tid, OwnerUserID: uid, Status: session.StatusActive}, nil
		},
		sendFn: func(ctx context.Context, _, _, _ uuid.UUID, _ string, on func(agent.Event) error) error {
			_ = on(agent.Event{Kind: agent.EventAssistantMessage, Step: 1, Text: "..."})
			// Simulate a short LLM run that must finish even if the client disconnects.
			select {
			case <-time.After(200 * time.Millisecond):
				done <- struct{}{}
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	base, tok, _ := newWSServer(t, svc)
	c := dialWS(t, base, sid.String(), tok)
	require.NoError(t, c.WriteJSON(map[string]string{"type": "user_message", "content": "x"}))
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var got map[string]json.RawMessage
	require.NoError(t, c.ReadJSON(&got))
	require.Equal(t, `"event"`, string(got["type"]))

	// Close the client early; SendMessage uses WithoutCancel and should still finish.
	_ = c.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SendMessage did not complete after client disconnect")
	}
}

func TestWS_UnknownFrame_ClosesConn(t *testing.T) {
	sid := uuid.New()
	svc := &mockWSSvc{
		getSessFn: func(_ context.Context, tid, uid, _ uuid.UUID) (*session.Session, error) {
			return &session.Session{ID: sid, TenantID: tid, OwnerUserID: uid, Status: session.StatusActive}, nil
		},
	}
	base, tok, _ := newWSServer(t, svc)
	c := dialWS(t, base, sid.String(), tok)
	require.NoError(t, c.WriteJSON(map[string]string{"type": "gibberish"}))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := c.ReadMessage()
	require.Error(t, err)
	// Verify it's a close error (not a timeout).
	require.True(t, strings.Contains(err.Error(), "unknown_frame_type") ||
		websocket.IsCloseError(err, websocket.CloseUnsupportedData),
		"want close 1003, got %v", err)
}

func TestWS_InvalidJSON_ClosesConn(t *testing.T) {
	sid := uuid.New()
	svc := &mockWSSvc{
		getSessFn: func(_ context.Context, tid, uid, _ uuid.UUID) (*session.Session, error) {
			return &session.Session{ID: sid, TenantID: tid, OwnerUserID: uid, Status: session.StatusActive}, nil
		},
	}
	base, tok, _ := newWSServer(t, svc)
	c := dialWS(t, base, sid.String(), tok)
	require.NoError(t, c.WriteMessage(websocket.TextMessage, []byte("not json")))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := c.ReadMessage()
	require.Error(t, err)
}

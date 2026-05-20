package session_test

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
	"github.com/yourorg/private-coding-agent/internal/session"
)

// mockHandlerSvc records the last call and replays a scripted response.
type mockHandlerSvc struct {
	createSession  func(context.Context, uuid.UUID, uuid.UUID, session.CreateRequest) (*session.Session, error)
	listSessions   func(context.Context, uuid.UUID, uuid.UUID) ([]session.Session, error)
	getSession     func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*session.Session, error)
	archiveSession func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	listMessages   func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]session.Message, error)
}

func (m *mockHandlerSvc) CreateSession(ctx context.Context, t, u uuid.UUID, r session.CreateRequest) (*session.Session, error) {
	return m.createSession(ctx, t, u, r)
}
func (m *mockHandlerSvc) ListSessions(ctx context.Context, t, u uuid.UUID) ([]session.Session, error) {
	return m.listSessions(ctx, t, u)
}
func (m *mockHandlerSvc) GetSession(ctx context.Context, t, u, id uuid.UUID) (*session.Session, error) {
	return m.getSession(ctx, t, u, id)
}
func (m *mockHandlerSvc) ArchiveSession(ctx context.Context, t, u, id uuid.UUID) error {
	return m.archiveSession(ctx, t, u, id)
}
func (m *mockHandlerSvc) ListMessages(ctx context.Context, t, u, id uuid.UUID) ([]session.Message, error) {
	return m.listMessages(ctx, t, u, id)
}

func newHandlerRouter(t *testing.T, svc session.HandlerService) (*gin.Engine, string, uuid.UUID, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	tid, uid := uuid.New(), uuid.New()
	tok, _ := j.Issue(uid, tid, "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	session.NewHandler(svc).Register(g)
	return r, "Bearer " + tok, tid, uid
}

func TestHandler_Create_OK(t *testing.T) {
	now := time.Now()
	svc := &mockHandlerSvc{
		createSession: func(_ context.Context, tid, uid uuid.UUID, req session.CreateRequest) (*session.Session, error) {
			require.Equal(t, "default-mock:gpt-4o", req.Model)
			return &session.Session{
				ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
				Model: req.Model, Profile: "coding", Status: session.StatusActive,
				CreatedAt: now, UpdatedAt: now,
			}, nil
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)

	body, _ := json.Marshal(map[string]any{"model": "default-mock:gpt-4o", "title": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var got session.Session
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "default-mock:gpt-4o", got.Model)
	require.Equal(t, session.StatusActive, got.Status)
}

func TestHandler_Create_ModelRequired(t *testing.T) {
	svc := &mockHandlerSvc{
		createSession: func(context.Context, uuid.UUID, uuid.UUID, session.CreateRequest) (*session.Session, error) {
			return nil, session.ErrModelRequired
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "model_required")
}

func TestHandler_Create_Unauthorized(t *testing.T) {
	r, _, _, _ := newHandlerRouter(t, &mockHandlerSvc{})
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_List_OK(t *testing.T) {
	svc := &mockHandlerSvc{
		listSessions: func(_ context.Context, tid, uid uuid.UUID) ([]session.Session, error) {
			return []session.Session{
				{ID: uuid.New(), TenantID: tid, OwnerUserID: uid, Model: "m", Status: session.StatusActive},
				{ID: uuid.New(), TenantID: tid, OwnerUserID: uid, Model: "m", Status: session.StatusActive},
			}, nil
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Sessions []session.Session `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Sessions, 2)
}

func TestHandler_Get_OK(t *testing.T) {
	sid := uuid.New()
	svc := &mockHandlerSvc{
		getSession: func(_ context.Context, tid, uid, id uuid.UUID) (*session.Session, error) {
			require.Equal(t, sid, id)
			return &session.Session{ID: sid, TenantID: tid, OwnerUserID: uid, Model: "m", Status: session.StatusActive}, nil
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sid.String(), nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandler_Get_NotFound(t *testing.T) {
	svc := &mockHandlerSvc{
		getSession: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*session.Session, error) {
			return nil, session.ErrSessionNotFound
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "not_found")
}

func TestHandler_Get_BadID(t *testing.T) {
	r, tok, _, _ := newHandlerRouter(t, &mockHandlerSvc{})
	req := httptest.NewRequest(http.MethodGet, "/sessions/not-a-uuid", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Archive_OK(t *testing.T) {
	called := false
	svc := &mockHandlerSvc{
		archiveSession: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
			called = true
			return nil
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodDelete, "/sessions/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, called)
}

func TestHandler_Archive_NotFound(t *testing.T) {
	svc := &mockHandlerSvc{
		archiveSession: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
			return session.ErrSessionNotFound
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodDelete, "/sessions/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_ListMessages_OK(t *testing.T) {
	sid := uuid.New()
	svc := &mockHandlerSvc{
		listMessages: func(_ context.Context, tid, uid, id uuid.UUID) ([]session.Message, error) {
			require.Equal(t, sid, id)
			return []session.Message{
				{ID: uuid.New(), SessionID: sid, TenantID: tid, Seq: 1, Role: session.RoleUser, Content: "hi"},
				{ID: uuid.New(), SessionID: sid, TenantID: tid, Seq: 2, Role: session.RoleAssistant, Content: "hello"},
			}, nil
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sid.String()+"/messages", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Messages []session.Message `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Messages, 2)
	require.Equal(t, "hi", resp.Messages[0].Content)
}

func TestHandler_ListMessages_NotFound(t *testing.T) {
	svc := &mockHandlerSvc{
		listMessages: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]session.Message, error) {
			return nil, session.ErrSessionNotFound
		},
	}
	r, tok, _, _ := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+uuid.New().String()+"/messages", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

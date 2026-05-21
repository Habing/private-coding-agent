package memory_test

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
	"github.com/yourorg/private-coding-agent/internal/memory"
)

// mockHandlerSvc replays scripted responses for handler-layer tests.
type mockHandlerSvc struct {
	create func(context.Context, uuid.UUID, uuid.UUID, memory.CreateRequest) (*memory.CreateResult, error)
	get    func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*memory.Memory, error)
	list   func(context.Context, uuid.UUID, uuid.UUID, memory.ListFilter) ([]memory.Memory, error)
	update func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, memory.UpdateRequest) (*memory.Memory, error)
	del    func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
}

func (m *mockHandlerSvc) Create(ctx context.Context, t, u uuid.UUID, r memory.CreateRequest) (*memory.CreateResult, error) {
	return m.create(ctx, t, u, r)
}
func (m *mockHandlerSvc) Get(ctx context.Context, t, u, id uuid.UUID) (*memory.Memory, error) {
	return m.get(ctx, t, u, id)
}
func (m *mockHandlerSvc) List(ctx context.Context, t, u uuid.UUID, f memory.ListFilter) ([]memory.Memory, error) {
	return m.list(ctx, t, u, f)
}
func (m *mockHandlerSvc) Update(ctx context.Context, t, u, id uuid.UUID, r memory.UpdateRequest) (*memory.Memory, error) {
	return m.update(ctx, t, u, id, r)
}
func (m *mockHandlerSvc) Delete(ctx context.Context, t, u, id uuid.UUID) error {
	return m.del(ctx, t, u, id)
}

func newHandlerRouter(t *testing.T, svc memory.HandlerService) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	tok, _ := j.Issue(uuid.New(), uuid.New(), "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	memory.NewHandler(svc).Register(g)
	return r, "Bearer " + tok
}

func TestHandler_Create_OK(t *testing.T) {
	svc := &mockHandlerSvc{
		create: func(_ context.Context, tid, uid uuid.UUID, req memory.CreateRequest) (*memory.CreateResult, error) {
			require.Equal(t, memory.TypePreference, req.Type)
			return &memory.CreateResult{Memory: &memory.Memory{
				ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
				Type: req.Type, Content: req.Content, Tags: req.Tags,
				Source: memory.SourceUser, LastUsedAt: time.Now(),
			}, Created: true}, nil
		},
	}
	r, tok := newHandlerRouter(t, svc)
	body, _ := json.Marshal(memory.CreateRequest{
		Type: memory.TypePreference, Content: "uses tabs", Tags: []string{"style"},
	})
	req := httptest.NewRequest(http.MethodPost, "/memories", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
}

func TestHandler_Create_Validation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"empty content", memory.ErrEmptyContent, "validation: content"},
		{"bad type", memory.ErrInvalidType, "validation: type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockHandlerSvc{
				create: func(context.Context, uuid.UUID, uuid.UUID, memory.CreateRequest) (*memory.CreateResult, error) {
					return nil, tc.err
				},
			}
			r, tok := newHandlerRouter(t, svc)
			req := httptest.NewRequest(http.MethodPost, "/memories", bytes.NewReader([]byte(`{"type":"profile","content":"x"}`)))
			req.Header.Set("Authorization", tok)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), tc.want)
		})
	}
}

func TestHandler_Create_Unauthorized(t *testing.T) {
	r, _ := newHandlerRouter(t, &mockHandlerSvc{})
	req := httptest.NewRequest(http.MethodPost, "/memories", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_List_OK_WithFilters(t *testing.T) {
	var captured memory.ListFilter
	svc := &mockHandlerSvc{
		list: func(_ context.Context, _, _ uuid.UUID, f memory.ListFilter) ([]memory.Memory, error) {
			captured = f
			return []memory.Memory{{ID: uuid.New(), Type: memory.TypeKnowledge, Content: "x"}}, nil
		},
	}
	r, tok := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/memories?type=knowledge&tag=go&q=Go", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, memory.TypeKnowledge, captured.Type)
	require.Equal(t, []string{"go"}, captured.Tags)
	require.Equal(t, "Go", captured.Query)

	var resp struct {
		Memories []memory.Memory `json:"memories"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Memories, 1)
}

func TestHandler_Get_NotFound(t *testing.T) {
	svc := &mockHandlerSvc{
		get: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*memory.Memory, error) {
			return nil, memory.ErrMemoryNotFound
		},
	}
	r, tok := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/memories/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "not_found")
}

func TestHandler_Get_BadID(t *testing.T) {
	r, tok := newHandlerRouter(t, &mockHandlerSvc{})
	req := httptest.NewRequest(http.MethodGet, "/memories/not-a-uuid", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Update_PartialFields(t *testing.T) {
	var captured memory.UpdateRequest
	svc := &mockHandlerSvc{
		update: func(_ context.Context, _, _, id uuid.UUID, r memory.UpdateRequest) (*memory.Memory, error) {
			captured = r
			return &memory.Memory{ID: id, Content: "new", Type: memory.TypeKnowledge}, nil
		},
	}
	r, tok := newHandlerRouter(t, svc)
	body := []byte(`{"content":"new","tags":[]}`)
	req := httptest.NewRequest(http.MethodPut, "/memories/"+uuid.New().String(), bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, captured.Content)
	require.Equal(t, "new", *captured.Content)
	require.True(t, captured.TagsSet)
	require.Empty(t, captured.Tags)
	require.Nil(t, captured.Type)
}

func TestHandler_Update_TagsNotSet(t *testing.T) {
	var captured memory.UpdateRequest
	svc := &mockHandlerSvc{
		update: func(_ context.Context, _, _, id uuid.UUID, r memory.UpdateRequest) (*memory.Memory, error) {
			captured = r
			return &memory.Memory{ID: id}, nil
		},
	}
	r, tok := newHandlerRouter(t, svc)
	body := []byte(`{"content":"x"}`)
	req := httptest.NewRequest(http.MethodPut, "/memories/"+uuid.New().String(), bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.False(t, captured.TagsSet)
}

func TestHandler_Delete_OK(t *testing.T) {
	called := false
	svc := &mockHandlerSvc{
		del: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
			called = true
			return nil
		},
	}
	r, tok := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodDelete, "/memories/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, called)
}

func TestHandler_Delete_NotFound(t *testing.T) {
	svc := &mockHandlerSvc{
		del: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
			return memory.ErrMemoryNotFound
		},
	}
	r, tok := newHandlerRouter(t, svc)
	req := httptest.NewRequest(http.MethodDelete, "/memories/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

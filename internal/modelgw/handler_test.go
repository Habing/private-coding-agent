package modelgw_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func newHandlerTestRouter(t *testing.T, mp *mockProvider) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gw, _, _ := gatewayWith(t, mp)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, _ := j.Issue(uid, tid, "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	modelgw.NewHandler(gw).Register(g)
	return r, "Bearer " + tok
}

func TestHandler_Chat_OK(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		chatRet: &modelgw.ChatResponse{
			ID: "x", Object: "chat.completion", Model: "m",
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "ok"},
			}},
		},
	}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.ChatRequest{
		Model:    "mock:m",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"content":"ok"`)
}

func TestHandler_Chat_NoAuth(t *testing.T) {
	r, _ := newHandlerTestRouter(t, &mockProvider{id: uuid.New(), name: "mock"})
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "mock:m",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Chat_BadModel(t *testing.T) {
	r, tok := newHandlerTestRouter(t, &mockProvider{id: uuid.New(), name: "mock"})
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "no-prefix",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), `"model_invalid"`)
}

func TestHandler_Chat_Stream(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		streamChunks: []modelgw.ChatStreamChunk{
			{ID: "x", Object: "chat.completion.chunk",
				Choices: []modelgw.ChatStreamChoice{{Delta: modelgw.ChatStreamDelta{Content: "a"}}}},
			{ID: "x", Object: "chat.completion.chunk",
				Choices: []modelgw.ChatStreamChoice{{Delta: modelgw.ChatStreamDelta{Content: "b"}}},
				Usage:   &modelgw.Usage{TotalTokens: 5}},
		},
	}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "mock:m", Stream: true,
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body2 := w.Body.String()
	require.True(t, strings.Contains(body2, `"content":"a"`))
	require.True(t, strings.Contains(body2, `"content":"b"`))
	require.True(t, strings.HasSuffix(body2, "data: [DONE]\n\n"))
}

func TestHandler_Embeddings_Unsupported(t *testing.T) {
	mp := &mockProvider{id: uuid.New(), name: "mock", embedErr: modelgw.ErrUnsupportedFeature}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.EmbeddingsRequest{Model: "mock:m", Input: []string{"hi"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "unsupported_feature")
}

func TestHandler_Chat_ProviderUnreachable_502(t *testing.T) {
	mp := &mockProvider{id: uuid.New(), name: "mock", chatErr: modelgw.ErrProviderUnreachable}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "mock:m",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "unreachable")
}

func TestHandler_Chat_Provider429(t *testing.T) {
	mp := &mockProvider{id: uuid.New(), name: "mock",
		chatErr: &modelgw.ProviderError{StatusCode: 429, Body: "rate limited"}}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "mock:m",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	// 注意: 上下文 imports
	_ = context.Background
}

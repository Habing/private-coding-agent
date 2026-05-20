package modelgw_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func newClaudeServer(t *testing.T, handler http.HandlerFunc) (*modelgw.ClaudeProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("TEST_CLAUDE_KEY", "sk-test-key")
	p, err := modelgw.NewClaudeProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test-claude", Type: "claude",
		BaseURL: srv.URL, APIKeyEnv: "TEST_CLAUDE_KEY",
	})
	require.NoError(t, err)
	return p, srv
}

func TestClaude_ChatCompletion_OK(t *testing.T) {
	p, _ := newClaudeServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "sk-test-key", r.Header.Get("x-api-key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		_, _ = w.Write([]byte(`{
			"id":"msg_1","type":"message","role":"assistant",
			"content":[{"type":"text","text":"hi from claude"}],
			"model":"claude-sonnet-4-5","stop_reason":"end_turn",
			"usage":{"input_tokens":5,"output_tokens":3}
		}`))
	})
	out, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{
			{Role: modelgw.RoleUser, Content: "hi"},
		}}, "claude-sonnet-4-5")
	require.NoError(t, err)
	require.Equal(t, "hi from claude", out.Choices[0].Message.Content)
	require.Equal(t, 8, out.Usage.TotalTokens)
	require.Equal(t, "test-claude:claude-sonnet-4-5", out.Model)
}

func TestClaude_ChatCompletion_4xx(t *testing.T) {
	p, _ := newClaudeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad"}}`))
	})
	_, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "x"}}},
		"claude-sonnet-4-5")
	require.Error(t, err)
	var pe *modelgw.ProviderError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, 400, pe.StatusCode)
}

func TestClaude_Embeddings_Unsupported(t *testing.T) {
	t.Setenv("TEST_CLAUDE_KEY", "x")
	p, err := modelgw.NewClaudeProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test-claude", BaseURL: "http://x",
		APIKeyEnv: "TEST_CLAUDE_KEY",
	})
	require.NoError(t, err)
	_, err = p.Embeddings(context.Background(),
		modelgw.EmbeddingsRequest{Input: []string{"hi"}}, "m")
	require.ErrorIs(t, err, modelgw.ErrUnsupportedFeature)
}

func TestClaude_Stream_TextOnly(t *testing.T) {
	p, _ := newClaudeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		write := func(s string) { _, _ = w.Write([]byte(s)); fl.Flush() }
		write("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n")
		write("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		write("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
		write("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		write("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		write("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	})

	var got []modelgw.ChatStreamChunk
	err := p.ChatCompletionStream(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "x"}}},
		"claude-sonnet-4-5",
		func(c modelgw.ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got), 3)
	require.Equal(t, "hi", got[1].Choices[0].Delta.Content)
	require.NotNil(t, got[len(got)-1].Usage)
}

// 拒绝 api_key_env 空时构造可以,但调用时返错。
func TestClaude_NoAPIKeyEnv_CallFails(t *testing.T) {
	p, err := modelgw.NewClaudeProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test", Type: "claude",
		BaseURL: "http://x", APIKeyEnv: "",
	})
	require.NoError(t, err)
	_, err = p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "x"}}},
		"m")
	require.Error(t, err)
	// 不暴露 env 名给客户端,但内部错误带提示
	require.Contains(t, err.Error(), "api_key_env")
}

var _ = json.RawMessage{}

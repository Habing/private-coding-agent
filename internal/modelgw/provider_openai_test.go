package modelgw_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func newOpenAIWithServer(t *testing.T, handler http.HandlerFunc) (*modelgw.OpenAIProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := modelgw.NewOpenAIProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test", Type: "openai", BaseURL: srv.URL,
	})
	require.NoError(t, err)
	return p, srv
}

func TestOpenAI_ChatCompletion_OK(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		_ = json.NewEncoder(w).Encode(modelgw.ChatResponse{
			ID: "x", Object: "chat.completion", Model: "gpt-4o",
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "hi"},
			}},
			Usage: modelgw.Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		})
	})
	out, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o")
	require.NoError(t, err)
	require.Equal(t, "hi", out.Choices[0].Message.Content)
	require.Equal(t, 6, out.Usage.TotalTokens)
}

func TestOpenAI_ChatCompletion_5xx(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o")
	require.Error(t, err)
	var pe *modelgw.ProviderError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, 500, pe.StatusCode)
}

func TestOpenAI_ChatCompletion_Unreachable(t *testing.T) {
	p, err := modelgw.NewOpenAIProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test", Type: "openai",
		BaseURL: "http://127.0.0.1:1",
	})
	require.NoError(t, err)
	_, err = p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o")
	require.ErrorIs(t, err, modelgw.ErrProviderUnreachable)
}

func TestOpenAI_Stream_ParsesChunks(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		write := func(s string) { _, _ = w.Write([]byte(s)); fl.Flush() }

		write("data: " + mustJSON(modelgw.ChatStreamChunk{
			ID: "1", Object: "chat.completion.chunk", Model: "gpt-4o",
			Choices: []modelgw.ChatStreamChoice{{Index: 0,
				Delta: modelgw.ChatStreamDelta{Content: "hi"},
			}},
		}) + "\n\n")
		write("data: " + mustJSON(modelgw.ChatStreamChunk{
			ID: "1", Object: "chat.completion.chunk", Model: "gpt-4o",
			Choices: []modelgw.ChatStreamChoice{},
			Usage:   &modelgw.Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		}) + "\n\n")
		write("data: [DONE]\n\n")
	})

	var got []modelgw.ChatStreamChunk
	err := p.ChatCompletionStream(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o",
		func(c modelgw.ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "hi", got[0].Choices[0].Delta.Content)
	require.NotNil(t, got[1].Usage)
	require.Equal(t, 6, got[1].Usage.TotalTokens)
}

func TestOpenAI_Stream_YieldErrorStops(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			_, _ = fmt.Fprintf(w, "data: %s\n\n",
				mustJSON(modelgw.ChatStreamChunk{Choices: []modelgw.ChatStreamChoice{{
					Delta: modelgw.ChatStreamDelta{Content: "x"},
				}}}))
			fl.Flush()
		}
	})

	myErr := errors.New("client gone")
	got := 0
	err := p.ChatCompletionStream(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o",
		func(c modelgw.ChatStreamChunk) error {
			got++
			if got == 2 {
				return myErr
			}
			return nil
		})
	require.ErrorIs(t, err, myErr)
	require.Equal(t, 2, got)
}

func TestOpenAI_Embeddings_OK(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		_ = json.NewEncoder(w).Encode(modelgw.EmbeddingsResponse{
			Object: "list",
			Data:   []modelgw.Embedding{{Index: 0, Object: "embedding", Embedding: []float64{0.1, 0.2}}},
			Model:  "text-embedding-3-small",
			Usage:  modelgw.Usage{PromptTokens: 1, TotalTokens: 1},
		})
	})
	out, err := p.Embeddings(context.Background(),
		modelgw.EmbeddingsRequest{Input: []string{"hi"}}, "text-embedding-3-small")
	require.NoError(t, err)
	require.Len(t, out.Data, 1)
	require.Equal(t, []float64{0.1, 0.2}, out.Data[0].Embedding)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// silence unused
var _ = strings.TrimSpace

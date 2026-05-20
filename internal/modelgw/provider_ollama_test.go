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

func TestOllama_TypeAndName(t *testing.T) {
	p, err := modelgw.NewOllamaProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "local-ollama", Type: "anything-overridden",
		BaseURL: "http://localhost:11434",
	})
	require.NoError(t, err)
	require.Equal(t, "ollama", p.Type())
	require.Equal(t, "local-ollama", p.Name())
}

func TestOllama_ChatCompletion_NoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(modelgw.ChatResponse{
			ID: "x", Object: "chat.completion", Model: "qwen2.5:7b",
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "yo"},
			}},
		})
	}))
	defer srv.Close()

	p, err := modelgw.NewOllamaProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "ollama", BaseURL: srv.URL, APIKeyEnv: "",
	})
	require.NoError(t, err)
	out, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"qwen2.5:7b")
	require.NoError(t, err)
	require.Equal(t, "yo", out.Choices[0].Message.Content)
}

package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

type mockGateway struct {
	chatRet   *modelgw.ChatResponse
	chatErr   error
	embedRet  *modelgw.EmbeddingsResponse
	embedErr  error
	lastChat  modelgw.ChatRequest
	lastEmbed modelgw.EmbeddingsRequest
}

func (m *mockGateway) ChatCompletion(_ context.Context, _, _ uuid.UUID, req modelgw.ChatRequest) (*modelgw.ChatResponse, error) {
	m.lastChat = req
	return m.chatRet, m.chatErr
}
func (m *mockGateway) Embeddings(_ context.Context, _, _ uuid.UUID, req modelgw.EmbeddingsRequest) (*modelgw.EmbeddingsResponse, error) {
	m.lastEmbed = req
	return m.embedRet, m.embedErr
}

func TestLLMChat_OK(t *testing.T) {
	gw := &mockGateway{
		chatRet: &modelgw.ChatResponse{
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "hi back"},
			}},
			Usage: modelgw.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		},
	}
	tool := tools.NewLLMChat(gw)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	var got struct {
		Content string        `json:"content"`
		Usage   modelgw.Usage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, "hi back", got.Content)
	require.Equal(t, 3, got.Usage.TotalTokens)
	require.Equal(t, "default-mock:gpt-4o", gw.lastChat.Model)
}

func TestLLMChat_ForwardsTemperature(t *testing.T) {
	gw := &mockGateway{
		chatRet: &modelgw.ChatResponse{Choices: []modelgw.ChatChoice{{}}},
	}
	tool := tools.NewLLMChat(gw)
	_, _ = tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"model":"x:y","messages":[{"role":"user","content":"x"}],"temperature":0.5}`))
	require.NotNil(t, gw.lastChat.Temperature)
	require.InDelta(t, 0.5, *gw.lastChat.Temperature, 0.001)
}

func TestLLMEmbed_OK(t *testing.T) {
	gw := &mockGateway{
		embedRet: &modelgw.EmbeddingsResponse{
			Data: []modelgw.Embedding{
				{Index: 0, Embedding: []float64{0.1, 0.2}},
				{Index: 1, Embedding: []float64{0.3, 0.4}},
			},
			Usage: modelgw.Usage{PromptTokens: 5, TotalTokens: 5},
		},
	}
	tool := tools.NewLLMEmbed(gw)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"model":"x:y","input":["a","b"]}`))
	require.NoError(t, err)
	var got struct {
		Vectors [][]float64   `json:"vectors"`
		Usage   modelgw.Usage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got.Vectors, 2)
	require.Equal(t, []float64{0.1, 0.2}, got.Vectors[0])
	require.Equal(t, []float64{0.3, 0.4}, got.Vectors[1])
}

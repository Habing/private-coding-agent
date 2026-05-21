package modelgw_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// mockProvider 满足 Provider, 行为可配置。
type mockProvider struct {
	id           uuid.UUID
	name         string
	chatRet      *modelgw.ChatResponse
	chatErr      error
	streamErr    error
	streamChunks []modelgw.ChatStreamChunk
	embedRet     *modelgw.EmbeddingsResponse
	embedErr     error
}

func (m *mockProvider) ID() uuid.UUID { return m.id }
func (m *mockProvider) Type() string  { return "mock" }
func (m *mockProvider) Name() string  { return m.name }
func (m *mockProvider) ChatCompletion(context.Context, modelgw.ChatRequest, string) (*modelgw.ChatResponse, error) {
	return m.chatRet, m.chatErr
}
func (m *mockProvider) ChatCompletionStream(_ context.Context, _ modelgw.ChatRequest, _ string,
	yield func(modelgw.ChatStreamChunk) error) error {
	for _, c := range m.streamChunks {
		if err := yield(c); err != nil {
			return err
		}
	}
	return m.streamErr
}
func (m *mockProvider) Embeddings(context.Context, modelgw.EmbeddingsRequest, string) (*modelgw.EmbeddingsResponse, error) {
	return m.embedRet, m.embedErr
}

func gatewayWith(t *testing.T, mp *mockProvider) (*modelgw.Gateway, *sync.Mutex, *[]error) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	var errs []error
	var mu sync.Mutex
	rec := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pg), func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})
	reg := modelgw.NewProviderRegistry(nil, nil, 0, true)
	reg.SeedForTest(map[string]modelgw.Provider{"mock": mp})
	return modelgw.NewGateway(reg, rec), &mu, &errs
}

func TestGateway_Chat_OK(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		chatRet: &modelgw.ChatResponse{
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "ok"},
			}},
			Usage: modelgw.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		},
	}
	gw, _, _ := gatewayWith(t, mp)

	out, err := gw.ChatCompletion(context.Background(),
		uuid.New(), uuid.New(),
		modelgw.ChatRequest{Model: "mock:m",
			Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	require.NoError(t, err)
	require.Equal(t, "ok", out.Choices[0].Message.Content)
	require.Equal(t, "mock:m", out.Model)
}

func TestGateway_Chat_ProviderUnreachable(t *testing.T) {
	mp := &mockProvider{id: uuid.New(), name: "mock", chatErr: modelgw.ErrProviderUnreachable}
	gw, _, _ := gatewayWith(t, mp)
	_, err := gw.ChatCompletion(context.Background(), uuid.New(), uuid.New(),
		modelgw.ChatRequest{Model: "mock:m",
			Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	require.ErrorIs(t, err, modelgw.ErrProviderUnreachable)
}

func TestGateway_Chat_BadModel(t *testing.T) {
	gw, _, _ := gatewayWith(t, &mockProvider{id: uuid.New(), name: "mock"})
	_, err := gw.ChatCompletion(context.Background(), uuid.New(), uuid.New(),
		modelgw.ChatRequest{Model: "noprefix",
			Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	require.ErrorIs(t, err, modelgw.ErrModelInvalid)
}

func TestGateway_Stream_RecordsLastUsage(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		streamChunks: []modelgw.ChatStreamChunk{
			{Choices: []modelgw.ChatStreamChoice{{Delta: modelgw.ChatStreamDelta{Content: "a"}}}},
			{Choices: []modelgw.ChatStreamChoice{{Delta: modelgw.ChatStreamDelta{Content: "b"}}},
				Usage: &modelgw.Usage{PromptTokens: 4, CompletionTokens: 6, TotalTokens: 10}},
		},
	}
	gw, _, _ := gatewayWith(t, mp)
	var got []modelgw.ChatStreamChunk
	err := gw.ChatCompletionStream(context.Background(), uuid.New(), uuid.New(),
		modelgw.ChatRequest{Model: "mock:m",
			Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		func(c modelgw.ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "mock:m", got[0].Model) // gateway 应回写 Model
}

func TestGateway_Embeddings_OK(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		embedRet: &modelgw.EmbeddingsResponse{
			Object: "list", Data: []modelgw.Embedding{{Embedding: []float64{1, 2}}},
			Usage: modelgw.Usage{PromptTokens: 1, TotalTokens: 1},
		},
	}
	gw, _, _ := gatewayWith(t, mp)
	out, err := gw.Embeddings(context.Background(), uuid.New(), uuid.New(),
		modelgw.EmbeddingsRequest{Model: "mock:m", Input: []string{"hi"}})
	require.NoError(t, err)
	require.Equal(t, "mock:m", out.Model)
}

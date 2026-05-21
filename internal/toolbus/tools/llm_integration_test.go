//go:build docker_integration

package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

func TestLLMChat_Integration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(modelgw.ChatResponse{
			ID: "x", Object: "chat.completion", Model: "m",
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "hello from mock"},
			}},
			Usage: modelgw.Usage{TotalTokens: 1},
		})
	}))
	defer srv.Close()

	p, err := modelgw.NewOpenAIProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "it-mock", Type: "openai", BaseURL: srv.URL,
	})
	require.NoError(t, err)

	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	reg := modelgw.NewProviderRegistry(nil, nil, 0, true)
	reg.SeedForTest(map[string]modelgw.Provider{"it-mock": p})
	rec := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pg), func(_ error) {})
	gw := modelgw.NewGateway(reg, rec)

	tool := tools.NewLLMChat(gw)
	in, _ := json.Marshal(map[string]any{
		"model":    "it-mock:m",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	out, err := tool.Invoke(ctx, uuid.New(), uuid.New(), in)
	require.NoError(t, err)
	require.Contains(t, string(out), "hello from mock")
}

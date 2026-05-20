package modelgw_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// fakeProvider 满足 Provider,但所有 Call 方法 panic;只测 Resolve 不调用业务。
type fakeProvider struct {
	id   uuid.UUID
	typ  string
	name string
}

func (f fakeProvider) ID() uuid.UUID { return f.id }
func (f fakeProvider) Type() string  { return f.typ }
func (f fakeProvider) Name() string  { return f.name }
func (f fakeProvider) ChatCompletion(context.Context, modelgw.ChatRequest, string) (*modelgw.ChatResponse, error) {
	panic("not for resolve tests")
}
func (f fakeProvider) ChatCompletionStream(context.Context, modelgw.ChatRequest, string,
	func(modelgw.ChatStreamChunk) error) error {
	panic("not for resolve tests")
}
func (f fakeProvider) Embeddings(context.Context, modelgw.EmbeddingsRequest, string) (*modelgw.EmbeddingsResponse, error) {
	panic("not for resolve tests")
}

// newRegistryWithSeed 跳过 PG / factory 直接给 registry 注入 byName。
func newRegistryWithSeed(byName map[string]modelgw.Provider) *modelgw.ProviderRegistry {
	r := modelgw.NewProviderRegistry(nil, nil, time.Minute)
	r.SeedForTest(byName)
	return r
}

func TestResolve_GoodModelStrings(t *testing.T) {
	cases := []struct {
		in   string
		want string // expected model (after prefix)
	}{
		{"openai:gpt-4o", "gpt-4o"},
		{"ollama:qwen2.5:7b", "qwen2.5:7b"},
		{"claude:claude-sonnet-4-5", "claude-sonnet-4-5"},
	}
	reg := newRegistryWithSeed(map[string]modelgw.Provider{
		"openai": fakeProvider{id: uuid.New(), typ: "openai", name: "openai"},
		"ollama": fakeProvider{id: uuid.New(), typ: "ollama", name: "ollama"},
		"claude": fakeProvider{id: uuid.New(), typ: "claude", name: "claude"},
	})
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			_, model, err := reg.Resolve(c.in)
			require.NoError(t, err)
			require.Equal(t, c.want, model)
		})
	}
}

func TestResolve_BadModelStrings(t *testing.T) {
	reg := newRegistryWithSeed(nil)
	for _, in := range []string{"", "noprefix", ":only-colon", "prefix:"} {
		_, _, err := reg.Resolve(in)
		require.ErrorIs(t, err, modelgw.ErrModelInvalid, "input: %q", in)
	}
}

func TestResolve_UnknownProvider(t *testing.T) {
	reg := newRegistryWithSeed(map[string]modelgw.Provider{
		"openai": fakeProvider{id: uuid.New(), typ: "openai", name: "openai"},
	})
	_, _, err := reg.Resolve("missing:m")
	require.ErrorIs(t, err, modelgw.ErrProviderNotFound)
}

// 用 dockertest PG seed 真 reload。
func TestRegistry_Start_LoadsSeed(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	repo := modelgw.NewProviderRepo(pg)
	reg := modelgw.NewProviderRegistry(repo, map[string]modelgw.ProviderFactory{
		"openai": func(cfg modelgw.ProviderConfig) (modelgw.Provider, error) {
			return fakeProvider{id: cfg.ID, typ: cfg.Type, name: cfg.Name}, nil
		},
	}, time.Minute)
	require.NoError(t, reg.Start(ctx))

	_, _, err = reg.Resolve("default-mock:gpt-4o")
	require.NoError(t, err)
}

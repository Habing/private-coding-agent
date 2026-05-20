package modelgw

import (
	"context"

	"github.com/google/uuid"
)

// Provider 是 LLM 后端抽象。所有方法协程安全。
// model 参数是裸 model 名(无 provider 前缀);Gateway 在 Resolve 后传入。
type Provider interface {
	ID() uuid.UUID
	Type() string
	Name() string

	ChatCompletion(ctx context.Context, req ChatRequest, model string) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, req ChatRequest, model string,
		yield func(ChatStreamChunk) error) error
	Embeddings(ctx context.Context, req EmbeddingsRequest, model string) (*EmbeddingsResponse, error)
}

// ProviderFactory 根据 ProviderConfig 构造 Provider 实例。
// 注册时一次性映射到 Type;Slice 3 含 "openai"/"ollama"/"claude" 三种工厂。
type ProviderFactory func(cfg ProviderConfig) (Provider, error)

// Package modelgw provides a Model Gateway abstraction over multiple LLM
// providers (Ollama, OpenAI, Claude) with OpenAI-compatible HTTP endpoints
// at /v1/chat/completions and /v1/embeddings.
package modelgw

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ChatRole 是 OpenAI 协议中的消息角色。
type ChatRole string

const (
	RoleSystem    ChatRole = "system"
	RoleUser      ChatRole = "user"
	RoleAssistant ChatRole = "assistant"
	RoleTool      ChatRole = "tool"
)

// ChatMessage 兼容 OpenAI Chat Completions message。
type ChatMessage struct {
	Role       ChatRole   `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall 是 OpenAI tool calling 格式。
type ToolCall struct {
	Index    int          `json:"index,omitempty"` // stream deltas only
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatRequest 兼容 OpenAI ChatCompletionRequest 子集。
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	Seed        *int          `json:"seed,omitempty"`
}

type ToolDef struct {
	Type     string          `json:"type"`
	Function ToolDefFunction `json:"function"`
}

type ToolDefFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   Usage        `json:"usage"`
}

type ChatStreamChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []ChatStreamChoice `json:"choices"`
	Usage   *Usage             `json:"usage,omitempty"`
}

type ChatStreamChoice struct {
	Index        int             `json:"index"`
	Delta        ChatStreamDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason,omitempty"`
}

type ChatStreamDelta struct {
	Role      ChatRole   `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type EmbeddingsRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions *int     `json:"dimensions,omitempty"` // OpenAI / DashScope optional output width
}

type Embedding struct {
	Index     int       `json:"index"`
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
}

type EmbeddingsResponse struct {
	Object string      `json:"object"`
	Data   []Embedding `json:"data"`
	Model  string      `json:"model"`
	Usage  Usage       `json:"usage"`
}

// 错误哨兵
var (
	ErrModelInvalid        = errors.New("model: must be 'provider:model'")
	ErrProviderNotFound    = errors.New("provider not found")
	ErrProviderUnreachable = errors.New("provider unreachable")
	ErrProviderError       = errors.New("provider returned error")
	ErrUnsupportedFeature  = errors.New("feature not supported by this provider")
)

// ProviderError 带 HTTP status code 与原始响应体（截断 4 KB）。
type ProviderError struct {
	StatusCode int
	Body       string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %d: %s", e.StatusCode, e.Body)
}

func (e *ProviderError) Is(target error) bool {
	return target == ErrProviderError
}

// 上限/默认
const (
	MaxMessages       = 200
	MaxMessageBytes   = 256 * 1024
	MaxEmbeddingInput = 100
	MaxEmbeddingItem  = 8 * 1024
	DefaultTimeoutSec = 120
	// MaxProviderBody caps each upstream provider response. A single 1536-d
	// embedding JSON-encodes to ~33 KB, so a batch of MaxEmbeddingInput (100)
	// can approach ~3.3 MB. 8 MB leaves comfortable headroom for both
	// embeddings batches and long-context chat completions.
	MaxProviderBody = 8 * 1024 * 1024
	StreamIdleTimeout = 60 * time.Second
	MaxStreamSeconds  = 600 * time.Second
)

// CallEvent 是 UsageRecorder 持久化用的领域对象。
type CallEvent struct {
	TenantID     uuid.UUID
	UserID       uuid.UUID
	ProviderID   uuid.UUID
	ProviderType string
	Model        string
	Action       string
	Stream       bool
	Status       string
	ErrorClass   string
	InputTokens  int
	OutputTokens int
	DurationMS   int64
	OccurredAt   time.Time
}

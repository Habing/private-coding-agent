package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// Gateway is the subset of modelgw.Gateway used by llm.* tools.
type Gateway interface {
	ChatCompletion(ctx context.Context, tenantID, userID uuid.UUID, req modelgw.ChatRequest) (*modelgw.ChatResponse, error)
	Embeddings(ctx context.Context, tenantID, userID uuid.UUID, req modelgw.EmbeddingsRequest) (*modelgw.EmbeddingsResponse, error)
}

// ---------- llm.chat ----------

type llmChat struct{ gw Gateway }

func NewLLMChat(gw Gateway) toolbus.Tool { return &llmChat{gw: gw} }

func (t *llmChat) Name() string { return "llm.chat" }
func (t *llmChat) Description() string {
	return "Send a Chat Completion request to the configured LLM provider. Returns the assistant message."
}
func (t *llmChat) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "model":{"type":"string"},
            "messages":{"type":"array","items":{
                "type":"object",
                "properties":{
                    "role":{"type":"string","enum":["system","user","assistant","tool"]},
                    "content":{"type":"string"}
                },
                "required":["role","content"]
            }},
            "temperature":{"type":"number"},
            "max_tokens":{"type":"integer"}
        },
        "required":["model","messages"],
        "additionalProperties":false
    }`)
}

type llmChatIn struct {
	Model       string                `json:"model"`
	Messages    []modelgw.ChatMessage `json:"messages"`
	Temperature *float64              `json:"temperature,omitempty"`
	MaxTokens   *int                  `json:"max_tokens,omitempty"`
}

func (t *llmChat) Invoke(ctx context.Context, tenantID, userID uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in llmChatIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	req := modelgw.ChatRequest{
		Model:       in.Model,
		Messages:    in.Messages,
		Temperature: in.Temperature,
		MaxTokens:   in.MaxTokens,
	}
	resp, err := t.gw.ChatCompletion(ctx, tenantID, userID, req)
	if err != nil {
		return nil, err
	}
	var content string
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	return json.Marshal(struct {
		Content string        `json:"content"`
		Usage   modelgw.Usage `json:"usage"`
	}{Content: content, Usage: resp.Usage})
}

// ---------- llm.embed ----------

type llmEmbed struct{ gw Gateway }

func NewLLMEmbed(gw Gateway) toolbus.Tool { return &llmEmbed{gw: gw} }

func (t *llmEmbed) Name() string { return "llm.embed" }
func (t *llmEmbed) Description() string {
	return "Compute embedding vectors for one or more text strings."
}
func (t *llmEmbed) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "model":{"type":"string"},
            "input":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":100}
        },
        "required":["model","input"],
        "additionalProperties":false
    }`)
}

type llmEmbedIn struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

func (t *llmEmbed) Invoke(ctx context.Context, tenantID, userID uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in llmEmbedIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	req := modelgw.EmbeddingsRequest{Model: in.Model, Input: in.Input}
	resp, err := t.gw.Embeddings(ctx, tenantID, userID, req)
	if err != nil {
		return nil, err
	}
	embs := append([]modelgw.Embedding(nil), resp.Data...)
	sort.Slice(embs, func(i, j int) bool { return embs[i].Index < embs[j].Index })
	vectors := make([][]float64, len(embs))
	for i, e := range embs {
		vectors[i] = e.Embedding
	}
	return json.Marshal(struct {
		Vectors [][]float64   `json:"vectors"`
		Usage   modelgw.Usage `json:"usage"`
	}{Vectors: vectors, Usage: resp.Usage})
}

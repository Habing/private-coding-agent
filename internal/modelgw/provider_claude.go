package modelgw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ClaudeProvider 通过 Anthropic Messages API 提供 OpenAI 兼容的 ChatCompletion。
// Embeddings 不支持(Anthropic 无官方 API),返 ErrUnsupportedFeature。
type ClaudeProvider struct {
	id        uuid.UUID
	name      string
	baseURL   string
	apiKeyEnv string
	client    *http.Client
}

func NewClaudeProvider(cfg ProviderConfig) (*ClaudeProvider, error) {
	return &ClaudeProvider{
		id:        cfg.ID,
		name:      cfg.Name,
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		apiKeyEnv: cfg.APIKeyEnv,
		client:    &http.Client{Timeout: time.Duration(DefaultTimeoutSec) * time.Second},
	}, nil
}

func (p *ClaudeProvider) ID() uuid.UUID { return p.id }
func (p *ClaudeProvider) Type() string  { return "claude" }
func (p *ClaudeProvider) Name() string  { return p.name }

func (p *ClaudeProvider) apiKey() (string, error) {
	if p.apiKeyEnv == "" {
		return "", fmt.Errorf("claude provider %q requires api_key_env", p.name)
	}
	v := os.Getenv(p.apiKeyEnv)
	if v == "" {
		return "", fmt.Errorf("api key env %q is empty", p.apiKeyEnv)
	}
	return v, nil
}

func (p *ClaudeProvider) newHTTPReq(ctx context.Context, body any, stream bool) (*http.Request, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	key, err := p.apiKey()
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", key)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

func (p *ClaudeProvider) ChatCompletion(ctx context.Context, req ChatRequest, model string) (*ChatResponse, error) {
	anthropicReq := ToAnthropicReq(req, model)
	anthropicReq.Stream = false

	hreq, err := p.newHTTPReq(ctx, anthropicReq, false)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(MaxProviderBody)))
	if resp.StatusCode >= 400 {
		return nil, &ProviderError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var ar anthropicMessagesResp
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	return FromAnthropicResp(ar, p.name, model), nil
}

func (p *ClaudeProvider) ChatCompletionStream(ctx context.Context, req ChatRequest, model string,
	yield func(ChatStreamChunk) error) error {
	anthropicReq := ToAnthropicReq(req, model)
	anthropicReq.Stream = true

	hreq, err := p.newHTTPReq(ctx, anthropicReq, true)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(hreq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(MaxProviderBody)))
		return &ProviderError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	return ConvertClaudeStream(resp.Body, p.name, model, yield)
}

func (p *ClaudeProvider) Embeddings(ctx context.Context, req EmbeddingsRequest, model string) (*EmbeddingsResponse, error) {
	return nil, ErrUnsupportedFeature
}

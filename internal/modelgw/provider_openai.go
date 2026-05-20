package modelgw

import (
	"bufio"
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

// OpenAIProvider 通过 HTTP 直连 OpenAI 兼容端点。
// Ollama 0.4+ 提供同样的 /v1/* 路径,所以 OllamaProvider 直接复用本结构。
type OpenAIProvider struct {
	id        uuid.UUID
	name      string
	typ       string // "openai" / "ollama" (用于 record)
	baseURL   string
	apiKeyEnv string
	client    *http.Client
}

func NewOpenAIProvider(cfg ProviderConfig) (*OpenAIProvider, error) {
	return &OpenAIProvider{
		id:        cfg.ID,
		name:      cfg.Name,
		typ:       cfg.Type, // 调用方传 "openai" 或 "ollama"
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		apiKeyEnv: cfg.APIKeyEnv,
		client:    &http.Client{Timeout: time.Duration(DefaultTimeoutSec) * time.Second},
	}, nil
}

func (p *OpenAIProvider) ID() uuid.UUID { return p.id }
func (p *OpenAIProvider) Type() string  { return p.typ }
func (p *OpenAIProvider) Name() string  { return p.name }

func (p *OpenAIProvider) apiKey() (string, error) {
	if p.apiKeyEnv == "" {
		return "", nil
	}
	v := os.Getenv(p.apiKeyEnv)
	if v == "" {
		return "", fmt.Errorf("api key env %q is empty", p.apiKeyEnv)
	}
	return v, nil
}

func (p *OpenAIProvider) newHTTPReq(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	key, err := p.apiKey()
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req, nil
}

func (p *OpenAIProvider) ChatCompletion(ctx context.Context, req ChatRequest, model string) (*ChatResponse, error) {
	upstream := req
	upstream.Model = model
	upstream.Stream = false

	hreq, err := p.newHTTPReq(ctx, http.MethodPost, "/v1/chat/completions", upstream)
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

	var out ChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

func (p *OpenAIProvider) ChatCompletionStream(ctx context.Context, req ChatRequest, model string,
	yield func(ChatStreamChunk) error) error {
	upstream := req
	upstream.Model = model
	upstream.Stream = true
	// OpenAI 需要显式打开 usage 在末帧
	type withUsage struct {
		ChatRequest
		StreamOptions map[string]any `json:"stream_options"`
	}
	bodyVal := withUsage{ChatRequest: upstream, StreamOptions: map[string]any{"include_usage": true}}

	hreq, err := p.newHTTPReq(ctx, http.MethodPost, "/v1/chat/completions", bodyVal)
	if err != nil {
		return err
	}
	hreq.Header.Set("Accept", "text/event-stream")
	resp, err := p.client.Do(hreq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(MaxProviderBody)))
		return &ProviderError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			return nil
		}
		var chunk ChatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 跳过坏帧
		}
		if err := yield(chunk); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	return nil
}

func (p *OpenAIProvider) Embeddings(ctx context.Context, req EmbeddingsRequest, model string) (*EmbeddingsResponse, error) {
	upstream := req
	upstream.Model = model

	hreq, err := p.newHTTPReq(ctx, http.MethodPost, "/v1/embeddings", upstream)
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

	var out EmbeddingsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

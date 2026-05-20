package modelgw

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Gateway 编排 validate → Resolve → Provider 调用 → record。
type Gateway struct {
	reg      *ProviderRegistry
	recorder *UsageRecorder
}

func NewGateway(reg *ProviderRegistry, recorder *UsageRecorder) *Gateway {
	return &Gateway{reg: reg, recorder: recorder}
}

func (g *Gateway) ChatCompletion(ctx context.Context, tenantID, userID uuid.UUID,
	req ChatRequest) (*ChatResponse, error) {
	if err := ValidateChatRequest(req); err != nil {
		return nil, err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, callErr := provider.ChatCompletion(ctx, req, model)
	g.record(tenantID, userID, provider, model, "chat", false, callErr,
		safeUsage(resp), time.Since(start))
	if callErr != nil {
		return nil, callErr
	}
	resp.Model = req.Model
	return resp, nil
}

func (g *Gateway) ChatCompletionStream(ctx context.Context, tenantID, userID uuid.UUID,
	req ChatRequest, yield func(ChatStreamChunk) error) error {
	if err := ValidateChatRequest(req); err != nil {
		return err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		return err
	}

	start := time.Now()
	var lastUsage *Usage
	wrapYield := func(c ChatStreamChunk) error {
		if c.Usage != nil {
			lastUsage = c.Usage
		}
		c.Model = req.Model
		return yield(c)
	}
	callErr := provider.ChatCompletionStream(ctx, req, model, wrapYield)
	g.record(tenantID, userID, provider, model, "chat", true, callErr,
		usagePtrOrZero(lastUsage), time.Since(start))
	return callErr
}

func (g *Gateway) Embeddings(ctx context.Context, tenantID, userID uuid.UUID,
	req EmbeddingsRequest) (*EmbeddingsResponse, error) {
	if err := ValidateEmbeddingsRequest(req); err != nil {
		return nil, err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, callErr := provider.Embeddings(ctx, req, model)
	g.record(tenantID, userID, provider, model, "embed", false, callErr,
		safeEmbedUsage(resp), time.Since(start))
	if callErr != nil {
		return nil, callErr
	}
	resp.Model = req.Model
	return resp, nil
}

func (g *Gateway) record(tenantID, userID uuid.UUID, p Provider, model, action string,
	stream bool, callErr error, usage Usage, dur time.Duration) {
	status := "ok"
	errClass := ""
	if callErr != nil {
		status = "error"
		errClass = classifyError(callErr)
	}
	g.recorder.Record(CallEvent{
		TenantID: tenantID, UserID: userID,
		ProviderID: p.ID(), ProviderType: p.Type(), Model: model,
		Action: action, Stream: stream,
		Status: status, ErrorClass: errClass,
		InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens,
		DurationMS: dur.Milliseconds(),
		OccurredAt: time.Now(),
	})
}

func classifyError(err error) string {
	switch {
	case errors.Is(err, ErrProviderUnreachable):
		return "unreachable"
	case errors.Is(err, ErrProviderError):
		return "provider_error"
	case errors.Is(err, ErrUnsupportedFeature):
		return "unsupported_feature"
	case errors.Is(err, ErrModelInvalid), errors.Is(err, ErrProviderNotFound):
		return "validation"
	}
	return "other"
}

func safeUsage(r *ChatResponse) Usage {
	if r == nil {
		return Usage{}
	}
	return r.Usage
}

func safeEmbedUsage(r *EmbeddingsResponse) Usage {
	if r == nil {
		return Usage{}
	}
	return r.Usage
}

func usagePtrOrZero(p *Usage) Usage {
	if p == nil {
		return Usage{}
	}
	return *p
}

// Registry returns the underlying registry (for handler-side pre-resolve checks).
func (g *Gateway) Registry() *ProviderRegistry { return g.reg }

package modelgw

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
)

var tracer trace.Tracer = otel.Tracer("internal/modelgw")

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
	ctx, span := tracer.Start(ctx, "model.chat",
		trace.WithAttributes(attribute.String("model.id", req.Model)))
	defer span.End()

	if err := ValidateChatRequest(req); err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}

	start := time.Now()
	resp, callErr := provider.ChatCompletion(ctx, req, model)
	g.record(tenantID, userID, provider, model, "chat", false, callErr,
		safeUsage(resp), time.Since(start))
	annotateModelSpan(span, safeUsage(resp), callErr)
	if callErr != nil {
		return nil, callErr
	}
	resp.Model = req.Model
	return resp, nil
}

func (g *Gateway) ChatCompletionStream(ctx context.Context, tenantID, userID uuid.UUID,
	req ChatRequest, yield func(ChatStreamChunk) error) error {
	ctx, span := tracer.Start(ctx, "model.chat_stream",
		trace.WithAttributes(attribute.String("model.id", req.Model)))
	defer span.End()

	if err := ValidateChatRequest(req); err != nil {
		recordSpanErr(span, err)
		return err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		recordSpanErr(span, err)
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
	annotateModelSpan(span, usagePtrOrZero(lastUsage), callErr)
	return callErr
}

func (g *Gateway) Embeddings(ctx context.Context, tenantID, userID uuid.UUID,
	req EmbeddingsRequest) (*EmbeddingsResponse, error) {
	ctx, span := tracer.Start(ctx, "model.embed",
		trace.WithAttributes(
			attribute.String("model.id", req.Model),
			attribute.Int("model.input_count", len(req.Input)),
		))
	defer span.End()

	if err := ValidateEmbeddingsRequest(req); err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}

	start := time.Now()
	resp, callErr := provider.Embeddings(ctx, req, model)
	g.record(tenantID, userID, provider, model, "embed", false, callErr,
		safeEmbedUsage(resp), time.Since(start))
	annotateModelSpan(span, safeEmbedUsage(resp), callErr)
	if callErr != nil {
		return nil, callErr
	}
	resp.Model = req.Model
	return resp, nil
}

func annotateModelSpan(span trace.Span, u Usage, err error) {
	if u.PromptTokens > 0 {
		span.SetAttributes(attribute.Int("model.prompt_tokens", u.PromptTokens))
	}
	if u.CompletionTokens > 0 {
		span.SetAttributes(attribute.Int("model.completion_tokens", u.CompletionTokens))
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

func recordSpanErr(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
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

	kind := action
	if stream {
		kind = action + "_stream"
	}
	ctx := context.Background()
	if pcametrics.ModelCallsTotal != nil {
		pcametrics.ModelCallsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("model", model),
			attribute.String("kind", kind),
			attribute.String("outcome", status),
		))
	}
	if pcametrics.ModelCallDuration != nil {
		pcametrics.ModelCallDuration.Record(ctx, dur.Seconds(),
			metric.WithAttributes(
				attribute.String("model", model),
				attribute.String("kind", kind),
			))
	}
	if pcametrics.ModelTokensTotal != nil {
		if usage.PromptTokens > 0 {
			pcametrics.ModelTokensTotal.Add(ctx, int64(usage.PromptTokens),
				metric.WithAttributes(
					attribute.String("model", model),
					attribute.String("direction", "in"),
				))
		}
		if usage.CompletionTokens > 0 {
			pcametrics.ModelTokensTotal.Add(ctx, int64(usage.CompletionTokens),
				metric.WithAttributes(
					attribute.String("model", model),
					attribute.String("direction", "out"),
				))
		}
	}
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

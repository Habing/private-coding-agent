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
	"github.com/yourorg/private-coding-agent/internal/quota"
)

var tracer trace.Tracer = otel.Tracer("internal/modelgw")

// Gateway 编排 validate → Resolve → Provider 调用 → record。
type Gateway struct {
	reg      *ProviderRegistry
	recorder *UsageRecorder
	quota    *quota.Service // optional; nil disables LLM token caps
}

func NewGateway(reg *ProviderRegistry, recorder *UsageRecorder) *Gateway {
	return &Gateway{reg: reg, recorder: recorder}
}

// WithQuota wires a quota.Service so chat/embeddings pre-check the
// per-tenant+user LLM-token cap and reconcile estimate-vs-actual after the
// call. nil keeps quota off. Returns the receiver for chaining.
func (g *Gateway) WithQuota(q *quota.Service) *Gateway {
	g.quota = q
	return g
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
	provider, model, err := g.reg.Resolve(tenantID, req.Model)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}

	estimate := estimateChatTokens(req)
	if g.quota != nil {
		if err := g.quota.CheckAndIncr(ctx, quota.KindLLMTokens, tenantID, userID, estimate); err != nil {
			recordSpanErr(span, err)
			return nil, err
		}
	}

	start := time.Now()
	resp, callErr := provider.ChatCompletion(ctx, req, model)
	usage := safeUsage(resp)
	g.reconcileLLMQuota(ctx, tenantID, userID, estimate, usage, callErr)
	g.record(tenantID, userID, provider, model, "chat", false, callErr,
		usage, time.Since(start))
	annotateModelSpan(span, usage, callErr)
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
	provider, model, err := g.reg.Resolve(tenantID, req.Model)
	if err != nil {
		recordSpanErr(span, err)
		return err
	}

	estimate := estimateChatTokens(req)
	if g.quota != nil {
		if err := g.quota.CheckAndIncr(ctx, quota.KindLLMTokens, tenantID, userID, estimate); err != nil {
			recordSpanErr(span, err)
			return err
		}
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
	usage := usagePtrOrZero(lastUsage)
	g.reconcileLLMQuota(ctx, tenantID, userID, estimate, usage, callErr)
	g.record(tenantID, userID, provider, model, "chat", true, callErr,
		usage, time.Since(start))
	annotateModelSpan(span, usage, callErr)
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
	provider, model, err := g.reg.Resolve(tenantID, req.Model)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}

	estimate := estimateEmbedTokens(req)
	if g.quota != nil {
		if err := g.quota.CheckAndIncr(ctx, quota.KindLLMTokens, tenantID, userID, estimate); err != nil {
			recordSpanErr(span, err)
			return nil, err
		}
	}

	start := time.Now()
	resp, callErr := provider.Embeddings(ctx, req, model)
	usage := safeEmbedUsage(resp)
	g.reconcileLLMQuota(ctx, tenantID, userID, estimate, usage, callErr)
	g.record(tenantID, userID, provider, model, "embed", false, callErr,
		usage, time.Since(start))
	annotateModelSpan(span, usage, callErr)
	if callErr != nil {
		return nil, callErr
	}
	resp.Model = req.Model
	return resp, nil
}

// estimateChatTokens returns a worst-case token reservation for a chat
// request: 512 prompt budget plus MaxTokens (or 1024 default if unset).
// Reconciled by reconcileLLMQuota after the call when actual usage arrives.
func estimateChatTokens(req ChatRequest) int {
	completion := 1024
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		completion = *req.MaxTokens
	}
	return 512 + completion
}

// estimateEmbedTokens approximates the input token count from raw char length
// (≈4 chars per token), floored at 64. Returns 0 reservation for empty input
// (validation rejects it earlier; defensive here).
func estimateEmbedTokens(req EmbeddingsRequest) int {
	total := 0
	for _, s := range req.Input {
		total += len(s)
	}
	est := total / 4
	if est < 64 {
		est = 64
	}
	return est
}

// reconcileLLMQuota applies a delta after the provider call to bring the
// Redis counter in line with actual usage. On success: delta = actual − est
// (may be negative). On failure: refund the entire estimate. Best-effort —
// errors are swallowed because the counter window will expire.
func (g *Gateway) reconcileLLMQuota(ctx context.Context, tenantID, userID uuid.UUID,
	estimate int, usage Usage, callErr error) {
	if g.quota == nil || estimate <= 0 {
		return
	}
	var delta int
	if callErr != nil {
		delta = -estimate
	} else if usage.TotalTokens > 0 {
		delta = usage.TotalTokens - estimate
	}
	if delta == 0 {
		return
	}
	_ = g.quota.Adjust(ctx, quota.KindLLMTokens, tenantID, userID, delta)
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

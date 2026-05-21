package logx

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// Field names for correlation attributes — fixed contract across all logs.
// Renames are a breaking change for downstream log indexes.
const (
	FieldRequestID = "request_id"
	FieldTraceID   = "trace_id"
	FieldSpanID    = "span_id"
	FieldTenantID  = "tenant_id"
	FieldUserID    = "user_id"
)

type ctxKey int

const (
	loggerCtxKey ctxKey = iota
	requestIDCtxKey
)

// WithLogger returns a child ctx that carries l. Used by middleware to install
// a per-request enriched logger so handlers downstream can FromCtx without
// re-deriving the same With(...) chain on every call.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey, l)
}

// WithRequestID stores id on ctx so FromCtx can include it in every record.
// The middleware in httpx is the canonical writer; callers should not set
// this manually outside of tests.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey, id)
}

// RequestIDFromCtx returns the request id stored on ctx, or "" if absent.
func RequestIDFromCtx(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(requestIDCtxKey).(string)
	return v
}

// FromCtx returns a logger enriched with whatever correlation fields are
// available on ctx: request_id, trace_id, span_id, tenant_id, user_id.
//
// The base logger comes from ctx if one was installed via WithLogger,
// otherwise the package default (set by Install). FromCtx is safe to call
// with a nil ctx — it returns the bare default.
func FromCtx(ctx context.Context) *slog.Logger {
	base := defaultLogger
	if ctx == nil {
		return base
	}
	if l, ok := ctx.Value(loggerCtxKey).(*slog.Logger); ok && l != nil {
		base = l
	}

	attrs := make([]any, 0, 10)
	if rid := RequestIDFromCtx(ctx); rid != "" {
		attrs = append(attrs, FieldRequestID, rid)
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		attrs = append(attrs, FieldTraceID, sc.TraceID().String(), FieldSpanID, sc.SpanID().String())
	}
	if cl := auth.FromCtx(ctx); cl != nil {
		attrs = append(attrs, FieldTenantID, cl.TenantID.String(), FieldUserID, cl.UserID.String())
	}
	if len(attrs) == 0 {
		return base
	}
	return base.With(attrs...)
}

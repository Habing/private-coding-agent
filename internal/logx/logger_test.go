package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func TestNew_DefaultsToJSONInfo(t *testing.T) {
	l := New(Config{})
	require.NotNil(t, l)
	require.True(t, l.Enabled(context.Background(), slog.LevelInfo))
	require.False(t, l.Enabled(context.Background(), slog.LevelDebug))
}

func TestNew_LevelFiltering(t *testing.T) {
	l := New(Config{Level: "warn"})
	require.True(t, l.Enabled(context.Background(), slog.LevelWarn))
	require.False(t, l.Enabled(context.Background(), slog.LevelInfo))
}

func TestFromCtx_NoCtxReturnsDefault(t *testing.T) {
	prev := defaultLogger
	t.Cleanup(func() { defaultLogger = prev })

	buf := &bytes.Buffer{}
	Install(slog.New(slog.NewJSONHandler(buf, nil)))

	FromCtx(nil).Info("hello")
	require.Contains(t, buf.String(), `"msg":"hello"`)
}

func TestFromCtx_EnrichesRequestID(t *testing.T) {
	prev := defaultLogger
	t.Cleanup(func() { defaultLogger = prev })

	buf := &bytes.Buffer{}
	Install(slog.New(slog.NewJSONHandler(buf, nil)))

	ctx := WithRequestID(context.Background(), "req-123")
	FromCtx(ctx).Info("hi")

	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec))
	require.Equal(t, "req-123", rec[FieldRequestID])
}

func TestFromCtx_EnrichesAuthClaims(t *testing.T) {
	prev := defaultLogger
	t.Cleanup(func() { defaultLogger = prev })

	buf := &bytes.Buffer{}
	Install(slog.New(slog.NewJSONHandler(buf, nil)))

	tid := uuid.New()
	uid := uuid.New()
	ctx := auth.WithClaims(context.Background(), &auth.Claims{
		TenantID: tid, UserID: uid, Role: "admin",
	})
	FromCtx(ctx).Info("hi")

	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec))
	require.Equal(t, tid.String(), rec[FieldTenantID])
	require.Equal(t, uid.String(), rec[FieldUserID])
}

func TestFromCtx_EnrichesTraceFromSpanContext(t *testing.T) {
	prev := defaultLogger
	t.Cleanup(func() { defaultLogger = prev })

	buf := &bytes.Buffer{}
	Install(slog.New(slog.NewJSONHandler(buf, nil)))

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	require.NoError(t, err)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	FromCtx(ctx).Info("hi")

	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec))
	require.Equal(t, traceID.String(), rec[FieldTraceID])
	require.Equal(t, spanID.String(), rec[FieldSpanID])
}

func TestWithLogger_ScopedLoggerOverridesDefault(t *testing.T) {
	prev := defaultLogger
	t.Cleanup(func() { defaultLogger = prev })

	defBuf := &bytes.Buffer{}
	Install(slog.New(slog.NewJSONHandler(defBuf, nil)))

	scopedBuf := &bytes.Buffer{}
	scoped := slog.New(slog.NewJSONHandler(scopedBuf, nil))

	ctx := WithLogger(context.Background(), scoped)
	FromCtx(ctx).Info("hello-scoped")

	require.Contains(t, scopedBuf.String(), "hello-scoped")
	require.Empty(t, defBuf.String())
}

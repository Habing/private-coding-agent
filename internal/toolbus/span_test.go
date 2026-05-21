package toolbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestBus_Invoke_EmitsSpan exercises both OK and error paths through the
// `tool.invoke` span. Combined into one test because otel-go's global tracer
// captures its underlying provider on first use — splitting into multiple
// tests with separate TracerProviders would route subsequent tests' spans
// back to the first test's recorder.
func TestBus_Invoke_EmitsSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)

	ok := &mockTool{
		name:      "span.ok",
		schema:    json.RawMessage(`{"type":"object"}`),
		invokeRet: json.RawMessage(`{"ok":true}`),
	}
	boom := &mockTool{
		name:      "span.boom",
		schema:    json.RawMessage(`{"type":"object"}`),
		invokeErr: errors.New("kaboom"),
	}
	bus, _, _ := busWith(t, ok, boom)

	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"span.ok", json.RawMessage(`{}`))
	require.NoError(t, err)

	_, err = bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"span.boom", json.RawMessage(`{}`))
	require.Error(t, err)

	spans := rec.Ended()
	require.Len(t, spans, 2)

	okSpan, errSpan := spans[0], spans[1]
	require.Equal(t, "tool.invoke", okSpan.Name())
	require.Equal(t, "tool.invoke", errSpan.Name())

	okAttrs := attrMap(okSpan.Attributes())
	require.Equal(t, "span.ok", okAttrs["tool.name"])
	require.Equal(t, "ok", okAttrs["tool.outcome"])

	errAttrs := attrMap(errSpan.Attributes())
	require.Equal(t, "span.boom", errAttrs["tool.name"])
	require.Equal(t, "error", errAttrs["tool.outcome"])
	require.NotEmpty(t, errSpan.Events(), "expected RecordError event on error path")
}

func attrMap(kvs []attribute.KeyValue) map[string]string {
	m := map[string]string{}
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
)

// installPromMeter wires a real Prometheus exporter against a fresh registry so
// the test can scrape the exposition output. Must run before metrics.Init.
func installPromMeter(t *testing.T) *prometheus.Registry {
	t.Helper()
	gin.SetMode(gin.TestMode)
	reg := prometheus.NewRegistry()
	exp, err := otelprom.New(otelprom.WithRegisterer(reg))
	require.NoError(t, err)
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exp))
	otel.SetMeterProvider(mp)
	return reg
}

func TestMetrics_InitAndHandlerExposesPCAMetrics(t *testing.T) {
	reg := installPromMeter(t)
	require.NoError(t, pcametrics.Init())

	// Touch each instrument so it surfaces in the scrape (counters with 0
	// observation are omitted by the Prom exporter).
	ctx := context.Background()
	pcametrics.HTTPRequestsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", "GET"),
		attribute.String("route", "/probe"),
		attribute.String("status_code", "200"),
	))
	pcametrics.ToolInvocationsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tool", "fs.list"),
		attribute.String("outcome", "ok"),
	))
	pcametrics.ModelCallsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("model", "mock"),
		attribute.String("kind", "chat"),
		attribute.String("outcome", "ok"),
	))
	pcametrics.ModelTokensTotal.Add(ctx, 5, metric.WithAttributes(
		attribute.String("model", "mock"),
		attribute.String("direction", "in"),
	))
	pcametrics.SandboxActive.Add(ctx, 1)
	pcametrics.WSConnectionsActive.Add(ctx, 1)
	pcametrics.SessionsCreatedTotal.Add(ctx, 1,
		metric.WithAttributes(attribute.String("profile", "coding")))

	r := gin.New()
	r.GET("/metrics", pcametrics.Handler(reg))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	for _, want := range []string{
		"pca_http_requests_total",
		"pca_tool_invocations_total",
		"pca_model_calls_total",
		"pca_model_tokens_total",
		"pca_sandbox_active",
		"pca_ws_connections_active",
		"pca_sessions_created_total",
	} {
		require.True(t, strings.Contains(body, want), "missing %s in /metrics body", want)
	}
}

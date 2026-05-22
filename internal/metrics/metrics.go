// Package metrics defines the application's Prometheus / OTel metric
// instruments and the /metrics HTTP handler.
//
// Init must be called once at startup AFTER telemetry.Setup so the global
// MeterProvider is wired. All instruments are lazily created and stored in
// package-level vars for direct call-site use:
//
//	metrics.HTTPRequestsTotal.Add(ctx, 1, metric.WithAttributes(...))
//
// Naming follows Prometheus conventions and uses the "pca_" prefix to clearly
// distinguish application metrics from runtime ones.
package metrics

import (
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "internal/metrics"

var (
	initOnce sync.Once
	initErr  error

	HTTPRequestsTotal      metric.Int64Counter
	HTTPRequestDuration    metric.Float64Histogram
	ToolInvocationsTotal   metric.Int64Counter
	ToolInvocationDuration metric.Float64Histogram
	ModelCallsTotal        metric.Int64Counter
	ModelCallDuration      metric.Float64Histogram
	ModelTokensTotal       metric.Int64Counter
	SandboxActive          metric.Int64UpDownCounter
	WSConnectionsActive    metric.Int64UpDownCounter
	SessionsCreatedTotal   metric.Int64Counter
	SkillLoadTotal         metric.Int64Counter
	SkillInjectionsTotal   metric.Int64Counter
	SkillInjectedChars     metric.Int64Histogram
	ReflectionProposalsTotal metric.Int64Counter
)

// Init creates all instrument handles against the current global MeterProvider.
// Safe to call multiple times; only the first call has effect.
func Init() error {
	initOnce.Do(func() {
		m := otel.Meter(meterName)
		initErr = build(m)
	})
	return initErr
}

func build(m metric.Meter) error {
	var err error
	if HTTPRequestsTotal, err = m.Int64Counter(
		"pca_http_requests_total",
		metric.WithDescription("HTTP requests served, by method/route/status_code."),
	); err != nil {
		return wrap("pca_http_requests_total", err)
	}
	if HTTPRequestDuration, err = m.Float64Histogram(
		"pca_http_request_duration_seconds",
		metric.WithDescription("HTTP request duration in seconds."),
		metric.WithUnit("s"),
	); err != nil {
		return wrap("pca_http_request_duration_seconds", err)
	}
	if ToolInvocationsTotal, err = m.Int64Counter(
		"pca_tool_invocations_total",
		metric.WithDescription("Tool invocations, by tool name and outcome."),
	); err != nil {
		return wrap("pca_tool_invocations_total", err)
	}
	if ToolInvocationDuration, err = m.Float64Histogram(
		"pca_tool_invocation_duration_seconds",
		metric.WithDescription("Tool invocation duration in seconds."),
		metric.WithUnit("s"),
	); err != nil {
		return wrap("pca_tool_invocation_duration_seconds", err)
	}
	if ModelCallsTotal, err = m.Int64Counter(
		"pca_model_calls_total",
		metric.WithDescription("Model gateway calls, by model/kind/outcome."),
	); err != nil {
		return wrap("pca_model_calls_total", err)
	}
	if ModelCallDuration, err = m.Float64Histogram(
		"pca_model_call_duration_seconds",
		metric.WithDescription("Model gateway call duration in seconds."),
		metric.WithUnit("s"),
	); err != nil {
		return wrap("pca_model_call_duration_seconds", err)
	}
	if ModelTokensTotal, err = m.Int64Counter(
		"pca_model_tokens_total",
		metric.WithDescription("Model tokens consumed, by model and direction (in|out)."),
	); err != nil {
		return wrap("pca_model_tokens_total", err)
	}
	if SandboxActive, err = m.Int64UpDownCounter(
		"pca_sandbox_active",
		metric.WithDescription("Number of currently active sandboxes."),
	); err != nil {
		return wrap("pca_sandbox_active", err)
	}
	if WSConnectionsActive, err = m.Int64UpDownCounter(
		"pca_ws_connections_active",
		metric.WithDescription("Number of currently open WebSocket connections."),
	); err != nil {
		return wrap("pca_ws_connections_active", err)
	}
	if SessionsCreatedTotal, err = m.Int64Counter(
		"pca_sessions_created_total",
		metric.WithDescription("Sessions created, by profile."),
	); err != nil {
		return wrap("pca_sessions_created_total", err)
	}
	if SkillLoadTotal, err = m.Int64Counter(
		"pca_skill_load_total",
		metric.WithDescription("Skill files loaded from disk, by outcome."),
	); err != nil {
		return wrap("pca_skill_load_total", err)
	}
	if SkillInjectionsTotal, err = m.Int64Counter(
		"pca_skill_injections_total",
		metric.WithDescription("Agent runs that injected at least one Skill, by truncated flag."),
	); err != nil {
		return wrap("pca_skill_injections_total", err)
	}
	if SkillInjectedChars, err = m.Int64Histogram(
		"pca_skill_injected_chars",
		metric.WithDescription("Characters of Skill content injected into an agent run."),
	); err != nil {
		return wrap("pca_skill_injected_chars", err)
	}
	if ReflectionProposalsTotal, err = m.Int64Counter(
		"pca_reflection_proposals_total",
		metric.WithDescription("Memory proposals emitted by the Reflection worker, by outcome (created|auto_approved|approved|rejected|dropped|llm_failed)."),
	); err != nil {
		return wrap("pca_reflection_proposals_total", err)
	}
	return nil
}

func wrap(name string, err error) error {
	return fmt.Errorf("metrics: create %s: %w", name, err)
}

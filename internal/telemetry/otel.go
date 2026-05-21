// Package telemetry wires OpenTelemetry trace + metrics providers and exposes
// a Setup that returns a shutdown function.
package telemetry

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds the configuration required to set up OpenTelemetry providers.
type Config struct {
	ServiceName  string
	OTLPEndpoint string // empty -> trace + OTLP metric exporters disabled
	// PromRegistry, when non-nil, attaches a Prometheus exporter as an
	// additional MeterProvider reader. Independent of OTLPEndpoint: the
	// /metrics endpoint should work even with no OTLP collector.
	PromRegistry prometheus.Registerer
}

// Setup wires global OpenTelemetry trace and metric providers. When
// Config.OTLPEndpoint is empty no OTLP exporters are installed; when
// Config.PromRegistry is nil no Prometheus reader is installed. With both
// empty/nil the call is a no-op.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if cfg.OTLPEndpoint == "" && cfg.PromRegistry == nil {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceNameKey.String(cfg.ServiceName),
	))
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	var tp *sdktrace.TracerProvider
	meterOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}

	if cfg.OTLPEndpoint != "" {
		traceExp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		)
		if err != nil {
			return nil, fmt.Errorf("trace exporter: %w", err)
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExp),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)

		metricExp, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithInsecure(),
			otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
		)
		if err != nil {
			return nil, fmt.Errorf("metric exporter: %w", err)
		}
		meterOpts = append(meterOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)))
	}

	if cfg.PromRegistry != nil {
		promExp, err := otelprom.New(otelprom.WithRegisterer(cfg.PromRegistry))
		if err != nil {
			return nil, fmt.Errorf("prometheus exporter: %w", err)
		}
		meterOpts = append(meterOpts, sdkmetric.WithReader(promExp))
	}

	mp := sdkmetric.NewMeterProvider(meterOpts...)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		if tp != nil {
			_ = tp.Shutdown(ctx)
		}
		_ = mp.Shutdown(ctx)
		return nil
	}, nil
}

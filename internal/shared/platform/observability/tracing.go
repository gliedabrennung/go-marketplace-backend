package observability

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type TracingConfig struct {
	Endpoint       string
	Insecure       bool
	SampleRatio    float64
	AlwaysSampleOn []string
	Service        string
	Version        string
	Env            string
}

type ShutdownFunc func(ctx context.Context) error

func SetupTracing(ctx context.Context, cfg TracingConfig) (ShutdownFunc, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res := resource.NewSchemaless(
		attribute.String("service.name", cfg.Service),
		attribute.String("service.version", cfg.Version),
		attribute.String("deployment.environment.name", cfg.Env),
	)

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(prioritySampler{
			prefixes: cfg.AlwaysSampleOn,
			base:     sdktrace.TraceIDRatioBased(cfg.SampleRatio),
		})),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

type prioritySampler struct {
	prefixes []string
	base     sdktrace.Sampler
}

func (s prioritySampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	for _, prefix := range s.prefixes {
		if prefix != "" && strings.HasPrefix(p.Name, prefix) {
			return sdktrace.AlwaysSample().ShouldSample(p)
		}
	}
	return s.base.ShouldSample(p)
}

func (s prioritySampler) Description() string {
	return "PrioritySampler{" + s.base.Description() + "}"
}

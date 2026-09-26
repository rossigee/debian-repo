// Package tracing provides OpenTelemetry OTLP tracing for debian-repo
package tracing

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	apitrace "go.opentelemetry.io/otel/trace"
)

var globalTracer apitrace.Tracer

func init() {
	globalTracer = otel.Tracer("debian-repo")
}

// Tracer returns the global OpenTelemetry tracer
func Tracer() apitrace.Tracer {
	return globalTracer
}

// InitTracer initializes OTLP tracing and returns a cleanup function
func InitTracer(ctx context.Context, serviceName, otlpHost, otlpPort string, sampler string, sampleRate float64) (func(context.Context) error, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(fmt.Sprintf("%s:%s", otlpHost, otlpPort)),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	var samplerImpl sdktrace.Sampler
	switch sampler {
	case "always":
		samplerImpl = sdktrace.AlwaysSample()
	case "never":
		samplerImpl = sdktrace.NeverSample()
	case "probabilistic":
		samplerImpl = sdktrace.TraceIDRatioBased(sampleRate)
	default:
		samplerImpl = sdktrace.AlwaysSample()
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("0.2.4"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(samplerImpl),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(provider)

	// Set up propagators for extracting trace context from HTTP headers (W3C Trace Context)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, // W3C Trace Context
		propagation.Baggage{},      // W3C Baggage
	))

	globalTracer = provider.Tracer("debian-repo")

	slog.Info("OTLP tracing initialized", "service", serviceName, "endpoint", fmt.Sprintf("%s:%s", otlpHost, otlpPort), "sampler", sampler)

	return func(ctx context.Context) error {
		return provider.Shutdown(ctx)
	}, nil
}

// StartSpan creates a new span with the given name and attributes
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, apitrace.Span) {
	return globalTracer.Start(ctx, name, apitrace.WithAttributes(attrs...))
}

// RecordError records an error in the current span
func RecordError(span apitrace.Span, err error) {
	if err != nil && span != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// SetAttribute sets an attribute on the span
func SetAttribute(span apitrace.Span, key string, value interface{}) {
	if span != nil {
		span.SetAttributes(attribute.String(key, fmt.Sprintf("%v", value)))
	}
}

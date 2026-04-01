package otel

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const shutdownTimeout = 5 * time.Second

// InitTracer bootstraps the OpenTelemetry tracing pipeline. It connects an
// OTLP/gRPC exporter to the given endpoint (e.g. "localhost:4317"), registers
// a global TracerProvider with the service name as a resource attribute, and
// sets up W3C TraceContext + Baggage propagation.
//
// Call the returned shutdown function (typically via defer) to flush pending
// spans before the process exits. Returns an error if the exporter or
// resource setup fails.
func InitTracer(ctx context.Context, serviceName, endpoint string) (shutdown func(), err error) {
	// It uses gRPC to send trace data to the specified endpoint
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: create OTLP exporter: %w", err)
	}

	res, err := resource.Merge(
		// It creates a default resource
		resource.Default(),
		// It creates a schemaless resource with the service name
		resource.NewSchemaless(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		// Batcher groups them together and sends them in chunks
		sdktrace.WithBatcher(exporter),
		// It sets the resource with the service name
		sdktrace.WithResource(res),
	)

	// It sets the tracer provider and the text map propagator
	otel.SetTracerProvider(tp)
	// ensures that if Service A calls Service B, they both show up in the same single trace
	// This allows the trace context and baggage to be propagated through the system
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// It returns a shutdown function that flushes pending spans before the process exits
	shutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}

	return shutdown, nil
}

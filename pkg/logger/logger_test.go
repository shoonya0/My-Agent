package logger

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestNew_smoke(t *testing.T) {
	log, closeFn := New("info")
	defer func() { _ = closeFn() }()
	log.Info("logger smoke test")
}

func TestNew_invalidLevel_defaultsToInfo(t *testing.T) {
	log, closeFn := New("not-a-real-level")
	defer func() { _ = closeFn() }()
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestWithTraceContext_noSpan(t *testing.T) {
	ctx := context.Background()
	fields := WithTraceContext(ctx)
	if fields != nil {
		t.Fatalf("WithTraceContext(empty ctx) = %v, want nil", fields)
	}
}

func TestWithTraceContext_withSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	tr := tp.Tracer("test")
	ctx, span := tr.Start(context.Background(), "op")
	defer span.End()
	fields := WithTraceContext(ctx)
	if len(fields) != 2 {
		t.Fatalf("expected 2 zap fields, got %d", len(fields))
	}
}

package logger

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	logDir  = "logs"
	logFile = "logs/app.log"
)

// New creates a *zap.Logger that writes structured JSON to both stdout and
// a log file. The log file is opened in append mode with shared read access,
// allowing concurrent viewing by text editors or tail commands.
// level should be one of zap's level strings: debug, info, warn, error, dpanic, panic, fatal.
//
// The returned closeFunc must be called during shutdown (typically deferred
// after zap.Logger.Sync) to release the log file handle.
func New(level string) (*zap.Logger, func() error) {
	zapLevel := parseLevel(level)

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		panic("logger: failed to create logs directory: " + err.Error())
	}

	// Open file in truncate mode to clear old logs on restart
	// O_TRUNC clears existing content, O_CREATE creates if doesn't exist
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		panic("logger: failed to open log file: " + err.Error())
	}

	closeFunc := func() error {
		return file.Close()
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeLevel = zapcore.LowercaseLevelEncoder
	encCfg.EncodeCaller = zapcore.FullCallerEncoder

	jsonEncoder := zapcore.NewJSONEncoder(encCfg)
	consoleEncoder := zapcore.NewConsoleEncoder(encCfg)

	core := zapcore.NewTee(
		zapcore.NewCore(jsonEncoder, zapcore.AddSync(file), zapLevel),
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), zapLevel),
	)

	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), closeFunc
}

func parseLevel(level string) zapcore.Level {
	var l zapcore.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return zapcore.InfoLevel
	}
	return l
}

// WithTraceContext extracts the trace ID and span ID from the given context
// and returns them as zap fields for log-trace correlation.
// If the context does not contain a valid span, it returns an empty slice.
//
// Usage example:
//
//	logger.Info("processing request",
//	    zap.String("user_id", userID),
//	    logger.WithTraceContext(ctx)...,
//	)
func WithTraceContext(ctx context.Context) []zap.Field {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", span.SpanContext().TraceID().String()),
		zap.String("span_id", span.SpanContext().SpanID().String()),
	}
}

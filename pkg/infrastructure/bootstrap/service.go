package bootstrap

import (
	"context"
	"fmt"
	"os"

	"myAgent/internal/config"
	"myAgent/pkg/logger"
	apmotel "myAgent/pkg/infrastructure/otel"
	"myAgent/pkg/types"

	"go.uber.org/zap"
)

// ServiceContext holds common service dependencies initialized by bootstrap functions.
type ServiceContext struct {
	Config   *types.Config
	Log      *zap.Logger
	Shutdown func() error
}

// InitService performs standard initialization for any service:
// - Loads configuration
// - Creates structured logger
// - Initializes OpenTelemetry tracer
// Returns a ServiceContext with cleanup function and error if initialization fails.
func InitService(serviceName string) (*ServiceContext, error) {
	cfg := config.Load()
	log, closeLog := logger.New(cfg.LogLevel)

	shutdown, err := apmotel.InitTracer(context.Background(), serviceName, cfg.JaegerEndpoint)
	if err != nil {
		_ = log.Sync()
		_ = closeLog()
		return nil, fmt.Errorf("failed to initialize tracer: %w", err)
	}

	cleanupFunc := func() error {
		_ = log.Sync()
		if logErr := closeLog(); logErr != nil {
			fmt.Fprintf(os.Stderr, "logger: close log file: %v\n", logErr)
		}
		shutdown()
		return nil
	}

	return &ServiceContext{
		Config:   cfg,
		Log:      log,
		Shutdown: cleanupFunc,
	}, nil
}

// InitServiceSimple performs minimal initialization without OpenTelemetry:
// - Loads configuration
// - Creates structured logger
// Returns a ServiceContext with cleanup function and error if initialization fails.
// Use this for tools like cmd/migrate that don't need distributed tracing.
func InitServiceSimple() (*ServiceContext, error) {
	cfg := config.Load()
	log, closeLog := logger.New(cfg.LogLevel)

	cleanupFunc := func() error {
		_ = log.Sync()
		if logErr := closeLog(); logErr != nil {
			return fmt.Errorf("logger: close log file: %w", logErr)
		}
		return nil
	}

	return &ServiceContext{
		Config:   cfg,
		Log:      log,
		Shutdown: cleanupFunc,
	}, nil
}

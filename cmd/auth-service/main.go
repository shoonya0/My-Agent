package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"myAgent/internal/auth"
	"myAgent/internal/config"
	"myAgent/pkg/grpcserver"
	"myAgent/pkg/logger"
	"myAgent/pkg/mysql"
	apmotel "myAgent/pkg/otel"
	"myAgent/pkg/redis"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const serviceName = "auth-service"

func main() {
	// it loads the configuration from the environment variables
	cfg := config.Load()

	// it creates a new logger instance
	log, closeLog := logger.New(cfg.LogLevel)
	defer func() {
		_ = log.Sync()
		if err := closeLog(); err != nil {
			fmt.Fprintf(os.Stderr, "logger: close log file: %v\n", err)
		}
	}()

	log.Info("Starting auth-service",
		zap.String("service", serviceName),
		zap.String("grpc_port", cfg.GRPCPort),
		zap.String("log_level", cfg.LogLevel),
	)

	shutdown, err := apmotel.InitTracer(context.Background(), serviceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatal("Failed to initialise tracer", zap.Error(err))
	}
	defer shutdown()

	// it connects to the Redis database
	rdb := redis.InitRedis(cfg, log)
	defer rdb.Close()

	// it connects to the MySQL database
	db, err := mysql.NewDB(context.Background(), cfg.MySQLDSN)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()
	log.Info("Connected to MySQL")

	// Auto-migrate database tables
	if err := mysql.AutoMigrate(context.Background(), db); err != nil {
		log.Fatal("Failed to auto-migrate database", zap.Error(err))
	}
	log.Info("Database tables initialized successfully")

	// it creates a new auth repository using the MySQL database connection
	repo := auth.NewRepository(db)

	// it creates a new auth service using the auth repository and the Redis database connection

	svc := auth.NewService(repo, rdb, cfg, log)
	// it creates a new auth handler using the auth service
	h := auth.NewHandler(svc, log)

	// it starts the gRPC server using the auth handler
	grpcSrv, err := grpcserver.Start(cfg.GRPCPort, h.GRPCRegistrar(), log,
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(grpcserver.UnaryLoggingInterceptor(log)),
	)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}
	defer grpcSrv.GracefulStop()

	log.Info("auth-service ready (gRPC only)", zap.String("grpc_port", cfg.GRPCPort))

	// Block until shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("Shutdown signal received, stopping gRPC server")
}

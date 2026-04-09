package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"myAgent/internal/config"
	"myAgent/internal/orchestrator"
	"myAgent/pkg/grpcserver"
	"myAgent/pkg/kafka"
	"myAgent/pkg/llm"
	"myAgent/pkg/logger"
	"myAgent/pkg/mysql"
	apmotel "myAgent/pkg/otel"
	"myAgent/pkg/redis"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const serviceName = "orchestrator"

func main() {
	cfg := config.Load()
	log, closeLog := logger.New(cfg.LogLevel)
	defer func() {
		_ = log.Sync()
		if err := closeLog(); err != nil {
			fmt.Fprintf(os.Stderr, "logger: close log file: %v\n", err)
		}
	}()

	log.Info("Starting orchestrator",
		zap.String("service", serviceName),
		zap.String("grpc_port", cfg.OrchestratorGRPCPort),
		zap.String("log_level", cfg.LogLevel),
	)

	shutdown, err := apmotel.InitTracer(context.Background(), serviceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatal("Failed to initialise tracer", zap.Error(err))
	}
	defer shutdown()

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

	rdb := redis.InitRedis(cfg, log)
	defer rdb.Close()

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("Failed to create Kafka producer", zap.Error(err))
	}
	defer producer.Close()

	llmClient := llm.NewClient(cfg.OpenAIKey, cfg.OrchestratorModel, log)

	repo := orchestrator.NewRepository(db)
	svc := orchestrator.NewService(repo, producer, llmClient, rdb, log)
	h := orchestrator.NewHandler(svc, log)

	grpcOpts := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(grpcserver.UnaryLoggingInterceptor(log)),
	}

	grpcSrv, err := grpcserver.Start(cfg.OrchestratorGRPCPort, h.GRPCRegistrar(), log, grpcOpts...)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}
	defer grpcSrv.GracefulStop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("Shutdown signal received, stopping gRPC server")
}

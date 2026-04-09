package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"myAgent/internal/jobs/orchestrator"
	"myAgent/pkg/infrastructure/bootstrap"
	"myAgent/pkg/infrastructure/grpcserver"
	"myAgent/pkg/data/kafka"
	"myAgent/pkg/llm"
	"myAgent/pkg/data/mysql"
	"myAgent/pkg/data/redis"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const serviceName = "orchestrator"

func main() {
	svc, err := bootstrap.InitService(serviceName)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := svc.Shutdown(); err != nil {
			svc.Log.Error("Shutdown error", zap.Error(err))
		}
	}()

	cfg, log := svc.Config, svc.Log

	log.Info("Starting orchestrator",
		zap.String("service", serviceName),
		zap.String("grpc_port", cfg.OrchestratorGRPCPort),
		zap.String("log_level", cfg.LogLevel),
	)

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
	orchSvc := orchestrator.NewService(repo, producer, llmClient, rdb, log)
	h := orchestrator.NewHandler(orchSvc, log)

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

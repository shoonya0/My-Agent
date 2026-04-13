package main

import (
	"context"
	"os/signal"
	"syscall"

	"myAgent/api/approvalpb"
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
	"google.golang.org/grpc/credentials/insecure"
)

const serviceName = "orchestrator"

func dialApprovalService(addr string, log *zap.Logger) *grpc.ClientConn {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatal("Failed to dial approval-service", zap.String("addr", addr), zap.Error(err))
	}
	log.Info("Connected to approval-service gRPC", zap.String("addr", addr))
	return conn
}

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

	failedConsumer, err := orchestrator.NewJobFailedConsumer(cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("Failed to create job.failed Kafka consumer", zap.Error(err))
	}
	defer failedConsumer.Close()

	approvalConn := dialApprovalService(cfg.ApprovalServiceAddr, log)
	defer approvalConn.Close()
	approvalClient := approvalpb.NewApprovalServiceClient(approvalConn)

	llmClient := llm.NewClient(cfg.OpenAIKey, cfg.OrchestratorModel, log)

	repo := orchestrator.NewRepository(db)
	orchSvc := orchestrator.NewService(repo, producer, llmClient, rdb, approvalClient, log)
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := orchestrator.RunJobFailedConsumer(ctx, failedConsumer, orchSvc); err != nil && ctx.Err() == nil {
			log.Error("job.failed consumer stopped with error", zap.Error(err))
		}
	}()

	log.Info("orchestrator ready (gRPC + job.failed consumer)",
		zap.String("consumer_group", orchestrator.JobFailedConsumerGroupID),
	)

	<-ctx.Done()
	log.Info("Shutdown signal received, stopping orchestrator")
}

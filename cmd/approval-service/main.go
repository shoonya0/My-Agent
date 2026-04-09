package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"myAgent/internal/approval"
	"myAgent/internal/config"
	"myAgent/pkg/grpcserver"
	"myAgent/pkg/kafka"
	"myAgent/pkg/logger"
	apmotel "myAgent/pkg/otel"
	"myAgent/pkg/storage"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const (
	serviceName         = "approval-service"
	topicImageGenerated = "image.generated"
	consumerGroupID     = "approval-service"
)

func main() {
	cfg := config.Load()
	log, closeLog := logger.New(cfg.LogLevel)
	defer func() {
		_ = log.Sync()
		if err := closeLog(); err != nil {
			fmt.Fprintf(os.Stderr, "logger: close log file: %v\n", err)
		}
	}()

	shutdown, err := apmotel.InitTracer(context.Background(), serviceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatal("Failed to initialise tracer", zap.Error(err))
	}
	defer shutdown()

	// ---- Redis ----
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Redis connection failed", zap.Error(err))
	}
	log.Info("Connected to Redis", zap.String("addr", cfg.RedisAddr))

	// ---- Kafka ----
	consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, consumerGroupID, topicImageGenerated, log)
	if err != nil {
		log.Fatal("Failed to create Kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	// ---- S3 presigner (best-effort; nil disables presigning) ----
	presigner, err := storage.NewS3Presigner(
		context.Background(),
		cfg.AWSBucket,
		cfg.AWSEndpoint,
		cfg.AWSRegion,
		cfg.AWSAccessKeyID,
		cfg.AWSSecretAccessKey,
		log,
	)
	if err != nil {
		log.Warn("S3 presigner init failed; preview URLs will use raw object URLs",
			zap.Error(err),
		)
		presigner = nil
	}

	// ---- Stream manager for gRPC streaming ----
	streamManager := approval.NewStreamManager(log)

	// ---- Approval service ----
	svc := approval.NewService(
		consumer, streamManager, rdb,
		presigner, cfg.AWSBucket, cfg.AWSEndpoint,
		log,
	)

	// ---- gRPC handler ----
	grpcHandler := approval.NewGRPCHandler(streamManager, log)

	// ---- Start gRPC server ----
	grpcOpts := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(grpcserver.UnaryLoggingInterceptor(log)),
	}

	grpcSrv, err := grpcserver.Start(cfg.ApprovalGRPCPort, grpcHandler.GRPCRegistrar(), log, grpcOpts...)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}
	defer grpcSrv.GracefulStop()

	// ---- Start consumer in background ----
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := svc.ListenAndNotify(ctx); err != nil {
			log.Error("Consumer loop error", zap.Error(err))
		}
	}()

	log.Info("approval-service ready (gRPC + Kafka consumer)",
		zap.String("grpc_port", cfg.ApprovalGRPCPort),
		zap.String("topic", topicImageGenerated),
		zap.String("consumer_group", consumerGroupID),
	)

	// Block until shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("Shutdown signal received, stopping gRPC server and consumer")
	stop()

	log.Info("Approval-service shut down gracefully")
}

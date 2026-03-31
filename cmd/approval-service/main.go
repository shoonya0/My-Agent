package main

import (
	"context"
	"os/signal"
	"syscall"

	"myAgent/internal/approval"
	"myAgent/internal/config"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/kafka"
	"myAgent/pkg/logger"
	apmotel "myAgent/pkg/otel"
	"myAgent/pkg/storage"
	ws "myAgent/pkg/websocket"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	serviceName         = "approval-service"
	topicImageGenerated = "image.generated"
	consumerGroupID     = "approval-service"
)

func main() {
	cfg := config.Load()
	log := logger.New(cfg.LogLevel)
	defer log.Sync()

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

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("Failed to create Kafka producer", zap.Error(err))
	}
	defer producer.Close()

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

	// ---- WebSocket hub ----
	hub := ws.NewHub(log)
	go hub.Run()

	// ---- Approval service + handler ----
	svc := approval.NewService(
		consumer, producer, hub, rdb,
		presigner, cfg.AWSBucket, cfg.AWSEndpoint,
		log,
	)
	h := approval.NewHandler(svc, hub, cfg.JWTSecret, rdb, log)

	// ---- Start consumer in background ----
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := svc.ListenAndNotify(ctx); err != nil {
			log.Error("Consumer loop error", zap.Error(err))
		}
	}()

	log.Info("Starting approval-service",
		zap.String("port", cfg.Port),
		zap.String("topic", topicImageGenerated),
		zap.String("consumer_group", consumerGroupID),
	)

	// ---- HTTP server (blocks until signal) ----
	if err := httpserver.Start(":"+cfg.Port, h.Routes()); err != nil {
		log.Fatal("HTTP server error", zap.Error(err))
	}

	log.Info("Approval-service shut down gracefully")
}

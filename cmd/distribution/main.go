package main

import (
	"context"
	"os/signal"
	"syscall"

	"myAgent/internal/config"
	"myAgent/internal/credentials"
	"myAgent/internal/distribution"
	"myAgent/pkg/connectors"
	"myAgent/pkg/crypto"
	"myAgent/pkg/kafka"
	"myAgent/pkg/logger"
	"myAgent/pkg/mysql"
	apmotel "myAgent/pkg/otel"

	"go.uber.org/zap"
)

const (
	serviceName        = "distribution-service"
	topicImageApproved = "image.approved"
	consumerGroupID    = "distribution-service"
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

	// ---- MySQL ----
	db, err := mysql.NewDB(context.Background(), cfg.MySQLDSN)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()

	enc, err := crypto.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatal("Failed to create encryptor", zap.Error(err))
	}
	credRepo := credentials.NewRepository(db)
	credSvc := credentials.NewService(credRepo, enc)

	// ---- Kafka ----
	consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, consumerGroupID, topicImageApproved, log)
	if err != nil {
		log.Fatal("Failed to create Kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("Failed to create Kafka producer", zap.Error(err))
	}
	defer producer.Close()

	// ---- Connector registry ----
	registry := connectors.NewRegistry()
	registry.Register("instagram", connectors.NewInstagram(cfg.InstagramToken))
	registry.Register("whatsapp", connectors.NewWhatsApp(cfg.WhatsAppToken))
	registry.Register("discord", connectors.NewDiscord(cfg.DiscordWebhook))
	registry.Register("telegram", connectors.NewTelegram(cfg.TelegramToken))

	log.Info("Registered platform connectors",
		zap.Strings("platforms", registry.Platforms()),
	)

	// ---- Distribution service ----
	repo := distribution.NewRepository(db)
	svc := distribution.NewService(consumer, producer, registry, repo, credSvc, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("Starting distribution-service",
		zap.String("topic", topicImageApproved),
		zap.String("consumer_group", consumerGroupID),
	)

	if err := svc.Run(ctx); err != nil {
		log.Fatal("Distribution-service error", zap.Error(err))
	}

	log.Info("Distribution-service shut down gracefully")
}

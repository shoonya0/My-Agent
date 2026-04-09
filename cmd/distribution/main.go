package main

import (
	"context"
	"os/signal"
	"syscall"

	"myAgent/internal/platforms/credentials"
	"myAgent/internal/jobs/distribution"
	"myAgent/pkg/infrastructure/bootstrap"
	"myAgent/pkg/connectors"
	"myAgent/pkg/crypto"
	"myAgent/pkg/data/kafka"
	"myAgent/pkg/data/mysql"

	"go.uber.org/zap"
)

const (
	serviceName        = "distribution-service"
	topicImageApproved = "image.approved"
	consumerGroupID    = "distribution-service"
)

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

	// ---- MySQL ----
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
	registry.Register("instagram", connectors.NewInstagram())
	registry.Register("whatsapp", connectors.NewWhatsApp())
	registry.Register("discord", connectors.NewDiscord())
	registry.Register("telegram", connectors.NewTelegram())

	log.Info("Registered platform connectors",
		zap.Strings("platforms", registry.Platforms()),
	)

	// ---- Distribution service ----
	repo := distribution.NewRepository(db)
	distSvc := distribution.NewService(consumer, producer, registry, repo, credSvc, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("Starting distribution-service",
		zap.String("topic", topicImageApproved),
		zap.String("consumer_group", consumerGroupID),
	)

	if err := distSvc.Run(ctx); err != nil {
		log.Fatal("Distribution-service error", zap.Error(err))
	}

	log.Info("Distribution-service shut down gracefully")
}

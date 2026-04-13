package main

import (
	"context"
	"os/signal"
	"syscall"

	"myAgent/internal/workers/imagegenagent"
	"myAgent/pkg/infrastructure/bootstrap"
	"myAgent/pkg/comfyui"
	"myAgent/pkg/data/kafka"
	"myAgent/pkg/data/mysql"
	"myAgent/pkg/storage"

	"go.uber.org/zap"
)

const (
	serviceName        = "image-gen-agent"
	topicPromptRefined = "prompt.refined"
	consumerGroupID    = "image-gen-agent"
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

	db, err := mysql.NewDB(context.Background(), cfg.MySQLDSN)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()
	log.Info("Connected to MySQL")

	repo := imagegenagent.NewRepository(db)

	consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, consumerGroupID, topicPromptRefined, log)
	if err != nil {
		log.Fatal("Failed to create Kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("Failed to create Kafka producer", zap.Error(err))
	}
	defer producer.Close()

	comfyCli := comfyui.NewClient(cfg.ComfyUIBaseURL, log)

	uploader, err := storage.NewS3Uploader(
		context.Background(),
		cfg.AWSBucket,
		cfg.AWSEndpoint,
		cfg.AWSRegion,
		cfg.AWSAccessKeyID,
		cfg.AWSSecretAccessKey,
		log,
	)
	if err != nil {
		log.Fatal("Failed to initialise S3 uploader", zap.Error(err))
	}

	w := imagegenagent.NewWorker(consumer, producer, comfyCli, uploader, repo, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("Starting image-gen-agent",
		zap.String("topic", topicPromptRefined),
		zap.String("consumer_group", consumerGroupID),
		zap.String("comfyui_url", cfg.ComfyUIBaseURL),
		zap.String("s3_bucket", cfg.AWSBucket),
	)

	if err := w.Run(ctx); err != nil {
		log.Fatal("Image-gen-agent worker error", zap.Error(err))
	}

	log.Info("Image-gen-agent shut down gracefully")
}

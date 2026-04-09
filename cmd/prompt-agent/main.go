package main

import (
	"context"
	"os/signal"
	"syscall"

	"myAgent/internal/workers/promptagent"
	"myAgent/pkg/infrastructure/bootstrap"
	"myAgent/pkg/data/kafka"
	"myAgent/pkg/llm"

	"go.uber.org/zap"
)

const (
	serviceName                = "prompt-agent"
	topicPromptRefineRequested = "prompt.refine.requested"
	consumerGroupID            = "prompt-agent"
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

	consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, consumerGroupID, topicPromptRefineRequested, log)
	if err != nil {
		log.Fatal("Failed to create Kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("Failed to create Kafka producer", zap.Error(err))
	}
	defer producer.Close()

	refiner := llm.NewRefiner(cfg.OpenAIKey, cfg.PromptAgentModel, cfg.PromptAgentSystemPrompt, log)

	w := promptagent.NewWorker(consumer, producer, refiner, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("Starting prompt-agent",
		zap.String("topic", topicPromptRefineRequested),
		zap.String("consumer_group", consumerGroupID),
	)

	if err := w.Run(ctx); err != nil {
		log.Fatal("Prompt-agent worker error", zap.Error(err))
	}

	log.Info("Prompt-agent shut down gracefully")
}

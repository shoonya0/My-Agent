package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"myAgent/internal/config"
	"myAgent/internal/promptagent"
	"myAgent/pkg/kafka"
	"myAgent/pkg/llm"
	"myAgent/pkg/logger"
	apmotel "myAgent/pkg/otel"

	"go.uber.org/zap"
)

const (
	serviceName                = "prompt-agent"
	topicPromptRefineRequested = "prompt.refine.requested"
	consumerGroupID            = "prompt-agent"
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

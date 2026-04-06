package main

import (
	"context"

	"myAgent/internal/config"
	"myAgent/internal/orchestrator"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/kafka"
	"myAgent/pkg/llm"
	"myAgent/pkg/logger"
	"myAgent/pkg/mysql"
	apmotel "myAgent/pkg/otel"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

const serviceName = "orchestrator"

func main() {
	cfg := config.Load()
	log := logger.New(cfg.LogLevel)
	defer log.Sync()

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

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("Failed to create Kafka producer", zap.Error(err))
	}
	defer producer.Close()

	llmClient := llm.NewClient(cfg.OpenAIKey, cfg.OrchestratorModel, log)

	repo := orchestrator.NewRepository(db)
	svc := orchestrator.NewService(repo, producer, llmClient, log)
	h := orchestrator.NewHandler(svc, log)

	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	r.Use(otelgin.Middleware(serviceName))
	h.RegisterRoutes(r)

	log.Info("Starting orchestrator HTTP server", zap.String("port", cfg.OrchestratorPort))
	if err := httpserver.Start(":"+cfg.OrchestratorPort, r); err != nil {
		log.Fatal("HTTP server error", zap.Error(err))
	}
}

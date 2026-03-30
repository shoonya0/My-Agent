package main

import (
	"context"
	"myAgent/internal/apigateway"
	"myAgent/internal/config"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/logger"
	"myAgent/pkg/model"
	apmotel "myAgent/pkg/otel"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

func main() {
	cfg := config.Load()
	log := logger.New(cfg.LogLevel)
	defer log.Sync()

	shutdown, err := apmotel.InitTracer(context.Background(), cfg.ServiceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatal("Failed to initialise tracer", zap.Error(err))
	}
	defer shutdown()

	rdb := InitRedis(cfg, log)
	defer rdb.Close()

	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	r.Use(otelgin.Middleware(cfg.ServiceName))

	handler := apigateway.NewGatewayHandler(cfg, rdb)
	handler.RegisterRoutes(r)

	log.Info("Starting server", zap.String("port", cfg.Port))
	if err := httpserver.Start(":"+cfg.Port, r); err != nil {
		log.Fatal("Server error", zap.Error(err))
	}
}

func InitRedis(cfg *model.Config, log *zap.Logger) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to connect to Redis", zap.Error(err))
	}

	log.Info("Connected to Redis successfully")

	return rdb
}

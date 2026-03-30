package main

import (
	"context"
	"fmt"
	"log"
	"myAgent/internal/apigateway"
	"myAgent/internal/config"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/model"
	apmotel "myAgent/pkg/otel"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func main() {
	cfg := config.Load()

	shutdown, err := apmotel.InitTracer(context.Background(), cfg.ServiceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialise tracer: %v", err)
	}
	defer shutdown()

	rdb := InitRedis(cfg)
	defer rdb.Close()

	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	r.Use(otelgin.Middleware(cfg.ServiceName))

	handler := apigateway.NewGatewayHandler(cfg, rdb)
	handler.RegisterRoutes(r)

	if err := httpserver.Start(":"+cfg.Port, r); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func InitRedis(cfg *model.Config) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to connect to Redis: ", err)
	}

	fmt.Println("Connected to Redis successfully!")

	return rdb
}

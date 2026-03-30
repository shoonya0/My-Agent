package main

import (
	"context"
	"fmt"
	"log"
	"myAgent/internal/apigateway"
	"myAgent/internal/config"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/model"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var (
	RedisClient *redis.Client
	cfg         model.Config
)

func main() {

	// load config
	cfg = config.Load()

	// builds Redis client
	RedisClient = InitRedis(cfg)

	// start Gin server
	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})

	handler := apigateway.NewGatewayHandler(cfg, RedisClient)
	handler.RegisterRoutes(r)

	if err := httpserver.Start(":"+cfg.Port, r); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func InitRedis(cfg model.Config) *redis.Client {
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

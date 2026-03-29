package main

import (
	"context"
	"fmt"
	"log"
	"myAgent/internal/config"
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
	fmt.Printf("config loaded: %+v\n", cfg)

	r.Run(":" + cfg.Port)
}

func InitRedis(cfg model.Config) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,     // Redis server address
		Password: cfg.RedisPassword, // No password
		DB:       cfg.RedisDB,       // Use default DB
	})

	// Use Ping to verify the connection
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to connect to Redis: ", err)
	}

	fmt.Println("Connected to Redis successfully!")

	return rdb
}

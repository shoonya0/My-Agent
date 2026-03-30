package main

import (
	"context"
	"fmt"
	"log"
	"myAgent/internal/config"
	"myAgent/internal/middleware"
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

	routes(r)

	fmt.Println("Listening on http://localhost" + cfg.Port + "\n")

	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
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

func routes(r *gin.Engine) {
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "OK"})
	})

	public := r.Group("/api")
	public.POST("/register", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "OK"})
	})

	protected := r.Group("/api/v1")
	protected.Use(middleware.JWTMiddleware(cfg.JWTSecret, RedisClient))
	{
		protected.GET("/me", func(c *gin.Context) {
			user := middleware.CurrentUser(c)
			c.JSON(200, gin.H{
				"user_id": user.UserID,
				"roles":   user.Roles,
				"email":   user.Email,
			})
		})
	}
}

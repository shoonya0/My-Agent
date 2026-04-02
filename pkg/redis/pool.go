package redis

import (
	"context"
	"myAgent/pkg/model"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func InitRedis(cfg *model.Config, log *zap.Logger) *redis.Client {
	// it creates a new Redis connection pool
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// it pings the Redis server to verify connectivity
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to connect to Redis", zap.String("addr", cfg.RedisAddr), zap.Error(err))
	}

	log.Info("Connected to Redis", zap.String("addr", cfg.RedisAddr))

	return rdb
}

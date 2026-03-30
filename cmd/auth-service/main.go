package main

import (
	"context"

	"myAgent/internal/auth"
	"myAgent/internal/config"
	"myAgent/pkg/grpcserver"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/logger"
	"myAgent/pkg/model"
	apmotel "myAgent/pkg/otel"
	"myAgent/pkg/mysql"

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

	db, err := mysql.NewDB(context.Background(), cfg.MySQLDSN)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()

	rdb := initRedis(cfg, log)
	defer rdb.Close()

	repo := auth.NewRepository(db)
	svc := auth.NewService(repo, rdb, cfg.JWTSecret, log)
	h := auth.NewHandler(svc, log)

	grpcSrv, err := grpcserver.Start(cfg.GRPCPort, h.GRPCRegistrar(), log)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}
	defer grpcSrv.GracefulStop()

	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	r.Use(otelgin.Middleware(cfg.ServiceName))
	h.RegisterRoutes(r)

	log.Info("Starting auth HTTP server", zap.String("port", cfg.Port))
	if err := httpserver.Start(":"+cfg.Port, r); err != nil {
		log.Fatal("HTTP server error", zap.Error(err))
	}
}

func initRedis(cfg *model.Config, log *zap.Logger) *redis.Client {
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

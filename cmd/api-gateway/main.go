package main

import (
	"context"

	"myAgent/api/authpb"
	"myAgent/internal/apigateway"
	"myAgent/internal/config"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/logger"
	"myAgent/pkg/model"
	apmotel "myAgent/pkg/otel"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const serviceName = "api-gateway"

func main() {
	cfg := config.Load()
	log := logger.New(cfg.LogLevel)
	defer log.Sync()

	shutdown, err := apmotel.InitTracer(context.Background(), serviceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatal("Failed to initialise tracer", zap.Error(err))
	}
	defer shutdown()

	rdb := InitRedis(cfg, log)
	defer rdb.Close()

	authConn := dialAuthService(cfg, log)
	defer authConn.Close()
	authClient := authpb.NewAuthServiceClient(authConn)

	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	// It instruments the Gin HTTP server with OpenTelemetry it's used to track the HTTP requests and responses
	r.Use(otelgin.Middleware(serviceName))

	handler := apigateway.NewGatewayHandler(cfg, rdb, authClient)
	handler.RegisterRoutes(r)

	log.Info("Starting server", zap.String("port", cfg.Port))
	if err := httpserver.Start(":"+cfg.Port, r); err != nil {
		log.Fatal("Server error", zap.Error(err))
	}
}

func dialAuthService(cfg *model.Config, log *zap.Logger) *grpc.ClientConn {
	conn, err := grpc.NewClient(
		cfg.AuthServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatal("Failed to dial auth-service", zap.String("addr", cfg.AuthServiceAddr), zap.Error(err))
	}
	log.Info("Connected to auth-service gRPC", zap.String("addr", cfg.AuthServiceAddr))
	return conn
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

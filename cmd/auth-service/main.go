package main

import (
	"context"

	"myAgent/internal/auth"
	"myAgent/internal/config"
	"myAgent/pkg/grpcserver"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/logger"
	"myAgent/pkg/mysql"
	apmotel "myAgent/pkg/otel"
	"myAgent/pkg/redis"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

const serviceName = "auth-service"

func main() {
	// it loads the configuration from the environment variables
	cfg := config.Load()

	// it creates a new logger instance
	log := logger.New(cfg.LogLevel)
	// it syncs the logger
	defer log.Sync()

	// it initialises the tracer
	shutdown, err := apmotel.InitTracer(context.Background(), serviceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatal("Failed to initialise tracer", zap.Error(err))
	}
	defer shutdown()

	// it connects to the Redis database
	rdb := redis.InitRedis(cfg, log)
	defer rdb.Close()

	// it connects to the MySQL database
	db, err := mysql.NewDB(context.Background(), cfg.MySQLDSN)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()

	// it creates a new auth repository using the MySQL database connection
	repo := auth.NewRepository(db)

	// it creates a new auth service using the auth repository and the Redis database connection

	svc := auth.NewService(repo, rdb, cfg.JWTSecret, log)
	// it creates a new auth handler using the auth service
	h := auth.NewHandler(svc, log)

	// it starts the gRPC server using the auth handler
	grpcSrv, err := grpcserver.Start(cfg.GRPCPort, h.GRPCRegistrar(), log)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}
	defer grpcSrv.GracefulStop()

	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	r.Use(otelgin.Middleware(serviceName))
	h.RegisterRoutes(r)

	log.Info("Starting auth HTTP server", zap.String("port", cfg.Port))
	if err := httpserver.Start(":"+cfg.Port, r); err != nil {
		log.Fatal("HTTP server error", zap.Error(err))
	}
}

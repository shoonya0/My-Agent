package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"myAgent/internal/auth"
	"myAgent/pkg/infrastructure/bootstrap"
	"myAgent/pkg/infrastructure/grpcserver"
	"myAgent/pkg/data/mysql"
	"myAgent/pkg/data/redis"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const serviceName = "auth-service"

func main() {
	svc, err := bootstrap.InitService(serviceName)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := svc.Shutdown(); err != nil {
			svc.Log.Error("Shutdown error", zap.Error(err))
		}
	}()

	cfg, log := svc.Config, svc.Log

	log.Info("Starting auth-service",
		zap.String("service", serviceName),
		zap.String("grpc_port", cfg.GRPCPort),
		zap.String("log_level", cfg.LogLevel),
	)

	// it connects to the Redis database
	rdb := redis.InitRedis(cfg, log)
	defer rdb.Close()

	// it connects to the MySQL database
	db, err := mysql.NewDB(context.Background(), cfg.MySQLDSN)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()
	log.Info("Connected to MySQL")

	// Auto-migrate database tables
	if err := mysql.AutoMigrate(context.Background(), db); err != nil {
		log.Fatal("Failed to auto-migrate database", zap.Error(err))
	}
	log.Info("Database tables initialized successfully")

	// it creates a new auth repository using the MySQL database connection
	repo := auth.NewRepository(db)

	// it creates a new auth service using the auth repository and the Redis database connection

	authSvc := auth.NewService(repo, rdb, cfg, log)
	// it creates a new auth handler using the auth service
	h := auth.NewHandler(authSvc, log)

	// it starts the gRPC server using the auth handler
	grpcSrv, err := grpcserver.Start(cfg.GRPCPort, h.GRPCRegistrar(), log,
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(grpcserver.UnaryLoggingInterceptor(log)),
	)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}
	defer grpcSrv.GracefulStop()

	log.Info("auth-service ready (gRPC only)", zap.String("grpc_port", cfg.GRPCPort))

	// Block until shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("Shutdown signal received, stopping gRPC server")
}

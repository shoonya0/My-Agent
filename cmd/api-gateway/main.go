package main

import (
	"context"
	"fmt"
	"os"

	"myAgent/api/authpb"
	"myAgent/api/orchestratorpb"
	"myAgent/internal/apigateway"
	"myAgent/internal/config"
	"myAgent/internal/credentials"
	"myAgent/pkg/crypto"
	"myAgent/pkg/httpserver"
	"myAgent/pkg/logger"
	"myAgent/pkg/model"
	"myAgent/pkg/mysql"
	apmotel "myAgent/pkg/otel"
	"myAgent/pkg/redis"
	"myAgent/pkg/storage"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const serviceName = "api-gateway"

// client connection is used to make gRPC calls to the auth-service
func dialAuthService(cfg *model.Config, log *zap.Logger) *grpc.ClientConn {
	// it creates a new client connection to the auth-service
	conn, err := grpc.NewClient(
		cfg.AuthServiceAddr,
		// it uses insecure mode because we are not using TLS this helps us to avoid the need to generate a certificate and key
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// it uses the otelgrpc.NewClientHandler() to track the gRPC calls
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatal("Failed to dial auth-service", zap.String("addr", cfg.AuthServiceAddr), zap.Error(err))
	}
	log.Info("Connected to auth-service gRPC", zap.String("addr", cfg.AuthServiceAddr))
	return conn
}

func dialOrchestratorService(cfg *model.Config, log *zap.Logger) *grpc.ClientConn {
	conn, err := grpc.NewClient(
		cfg.OrchestratorServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatal("Failed to dial orchestrator", zap.String("addr", cfg.OrchestratorServiceAddr), zap.Error(err))
	}
	log.Info("Connected to orchestrator gRPC", zap.String("addr", cfg.OrchestratorServiceAddr))
	return conn
}

func main() {
	cfg := config.Load()
	log, closeLog := logger.New(cfg.LogLevel)
	defer func() {
		_ = log.Sync()
		if err := closeLog(); err != nil {
			fmt.Fprintf(os.Stderr, "logger: close log file: %v\n", err)
		}
	}()

	log.Info("Starting api-gateway",
		zap.String("service", serviceName),
		zap.String("port", cfg.APIGatewayPort),
		zap.String("log_level", cfg.LogLevel),
	)

	shutdown, err := apmotel.InitTracer(context.Background(), serviceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatal("Failed to initialise tracer", zap.Error(err))
	}
	defer shutdown()

	rdb := redis.InitRedis(cfg, log)
	defer rdb.Close()

	// it connects to the MySQL database
	db, err := mysql.NewDB(context.Background(), cfg.MySQLDSN)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()

	enc, err := crypto.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatal("Failed to create encryptor", zap.Error(err))
	}

	// it creates a new credentials repository using the MySQL database connection
	credRepo := credentials.NewRepository(db)

	// it creates a new credentials service using the credentials repository and the encryptor
	credSvc := credentials.NewService(credRepo, enc)

	// it creates a new credentials handler using the credentials service
	credHandler := credentials.NewHandler(credSvc)

	// it creates a new auth connection using the auth service address
	authConn := dialAuthService(cfg, log)
	defer authConn.Close()

	// it creates a new auth client using the auth connection that was created in the dialAuthService function
	authClient := authpb.NewAuthServiceClient(authConn)

	orchConn := dialOrchestratorService(cfg, log)
	defer orchConn.Close()
	orchClient := orchestratorpb.NewOrchestratorServiceClient(orchConn)

	uploader, err := storage.NewS3Uploader(
		context.Background(),
		cfg.AWSBucket,
		cfg.AWSEndpoint,
		cfg.AWSRegion,
		cfg.AWSAccessKeyID,
		cfg.AWSSecretAccessKey,
		log,
	)
	if err != nil {
		log.Fatal("Failed to create S3 uploader", zap.Error(err))
	}

	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})

	// it uses the otelgin middleware to trace the requests
	r.Use(otelgin.Middleware(serviceName))

	handler := apigateway.NewGatewayHandler(cfg, rdb, authClient, orchClient, uploader, credHandler)
	handler.RegisterRoutes(r, log)

	log.Info("HTTP server ready", zap.String("port", cfg.APIGatewayPort))
	if err := httpserver.Start(":"+cfg.APIGatewayPort, r); err != nil {
		log.Fatal("HTTP server error", zap.Error(err))
	}
}

package main

import (
	"context"

	"myAgent/api/authpb"
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

func main() {
	cfg := config.Load()
	log := logger.New(cfg.LogLevel)
	defer log.Sync()

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

	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	r.Use(otelgin.Middleware(serviceName))

	handler := apigateway.NewGatewayHandler(cfg, rdb, authClient, credHandler)
	handler.RegisterRoutes(r)

	log.Info("Starting server", zap.String("port", cfg.Port))
	if err := httpserver.Start(":"+cfg.Port, r); err != nil {
		log.Fatal("Server error", zap.Error(err))
	}
}

package main

import (
	"context"
	"time"

	"myAgent/api/approvalpb"
	"myAgent/api/authpb"
	"myAgent/api/orchestratorpb"
	"myAgent/internal/apigateway"
	"myAgent/internal/platforms/credentials"
	"myAgent/pkg/crypto"
	"myAgent/pkg/data/mysql"
	"myAgent/pkg/data/redis"
	"myAgent/pkg/infrastructure/bootstrap"
	"myAgent/pkg/infrastructure/httpserver"
	"myAgent/pkg/storage"
	"myAgent/pkg/types"
	ws "myAgent/pkg/websocket"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const serviceName = "api-gateway"

// subscribeToApprovalUpdates establishes a gRPC stream with approval-service
// to receive real-time job update notifications, then broadcasts them to
// connected WebSocket clients via the hub.
func subscribeToApprovalUpdates(ctx context.Context, client approvalpb.ApprovalServiceClient, hub *ws.Hub, nodeID string, log *zap.Logger) {
	retryDelay := 2 * time.Second
	maxRetryDelay := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			log.Info("Approval updates subscription stopping due to context cancellation")
			return
		default:
		}

		stream, err := client.SubscribeJobUpdates(ctx, &approvalpb.SubscribeRequest{
			NodeId: nodeID,
		})
		if err != nil {
			log.Error("Failed to subscribe to approval updates", zap.Error(err), zap.Duration("retry_in", retryDelay))

			select {
			case <-time.After(retryDelay):
				retryDelay = retryDelay * 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
				continue
			case <-ctx.Done():
				log.Info("Approval updates subscription stopping during retry")
				return
			}
		}

		log.Info("Subscribed to approval-service job updates stream", zap.String("node_id", nodeID))
		retryDelay = 2 * time.Second

		for {
			update, err := stream.Recv()
			if err != nil {
				log.Error("Stream receive error, reconnecting", zap.Error(err))
				break
			}

			wsNotification := types.WSNotification{
				Type:       update.NotificationType,
				JobID:      update.JobId,
				Status:     update.Status,
				Message:    update.Message,
				PreviewURL: update.PreviewUrl,
				Error:      update.Error,
			}

			if err := hub.SendToJob(update.JobId, wsNotification); err != nil {
				log.Warn("Failed to send WebSocket notification",
					zap.Error(err),
					zap.String("job_id", update.JobId),
				)
			}
		}
	}
}

// dialGRPCService creates a gRPC client connection to the specified service.
// It uses insecure credentials (no TLS) and OpenTelemetry instrumentation for tracing.
func dialGRPCService(serviceName, addr string, log *zap.Logger) *grpc.ClientConn {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatal("Failed to dial gRPC service",
			zap.String("service", serviceName),
			zap.String("addr", addr),
			zap.Error(err))
	}
	log.Info("Connected to gRPC service",
		zap.String("service", serviceName),
		zap.String("addr", addr))
	return conn
}

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

	log.Info("Starting api-gateway",
		zap.String("service", serviceName),
		zap.String("port", cfg.APIGatewayPort),
		zap.String("log_level", cfg.LogLevel),
	)

	rdb := redis.InitRedis(cfg, log)
	defer rdb.Close()

	// it connects to the MySQL database
	db, err := mysql.NewDB(context.Background(), cfg.MySQLDSN)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()
	log.Info("Connected to MySQL", zap.String("dsn", cfg.MySQLDSN))

	// Auto-migrate database tables
	if err := mysql.AutoMigrate(context.Background(), db); err != nil {
		log.Fatal("Failed to auto-migrate database", zap.Error(err))
	}
	log.Info("Database tables initialized successfully")

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

	// Create gRPC client connections to internal services
	authConn := dialGRPCService("auth-service", cfg.AuthServiceAddr, log)
	defer authConn.Close()
	authClient := authpb.NewAuthServiceClient(authConn)

	orchConn := dialGRPCService("orchestrator", cfg.OrchestratorServiceAddr, log)
	defer orchConn.Close()
	orchClient := orchestratorpb.NewOrchestratorServiceClient(orchConn)

	approvalConn := dialGRPCService("approval-service", cfg.ApprovalServiceAddr, log)
	defer approvalConn.Close()
	approvalClient := approvalpb.NewApprovalServiceClient(approvalConn)

	// Initialize WebSocket hub for real-time job updates
	hub := ws.NewHub(log)
	go hub.Run()

	// Subscribe to approval-service gRPC stream for job updates with cancellable context
	nodeID := uuid.New().String()
	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	go subscribeToApprovalUpdates(subCtx, approvalClient, hub, nodeID, log)

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

	handler := apigateway.NewGatewayHandler(cfg, rdb, authClient, orchClient, uploader, credHandler, hub)
	handler.RegisterRoutes(r, log)

	log.Info("HTTP server ready", zap.String("port", cfg.APIGatewayPort))
	if err := httpserver.Start(":"+cfg.APIGatewayPort, r); err != nil {
		log.Fatal("HTTP server error", zap.Error(err))
	}
}

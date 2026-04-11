package apigateway

import (
	"myAgent/api/authpb"
	"myAgent/api/orchestratorpb"
	"myAgent/internal/platforms/credentials"
	"myAgent/pkg/middleware/auth"
	"myAgent/pkg/middleware/ratelimit"
	"myAgent/pkg/storage"
	"myAgent/pkg/types"
	ws "myAgent/pkg/websocket"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const maxImageSize = 20 << 20 // 20 MB

// GatewayHandler holds dependencies for all api-gateway HTTP handlers.
type GatewayHandler struct {
	cfg         *types.Config
	rdb         *redis.Client
	authClient  authpb.AuthServiceClient
	orchClient  orchestratorpb.OrchestratorServiceClient
	uploader    storage.Uploader
	credHandler *credentials.Handler
	wsHub       *ws.Hub
}

// NewGatewayHandler constructs a GatewayHandler with the required dependencies.
func NewGatewayHandler(
	cfg *types.Config,
	rdb *redis.Client,
	authClient authpb.AuthServiceClient,
	orchClient orchestratorpb.OrchestratorServiceClient,
	uploader storage.Uploader,
	credHandler *credentials.Handler,
	wsHub *ws.Hub,
) *GatewayHandler {
	return &GatewayHandler{
		cfg:         cfg,
		rdb:         rdb,
		authClient:  authClient,
		orchClient:  orchClient,
		uploader:    uploader,
		credHandler: credHandler,
		wsHub:       wsHub,
	}
}

// RegisterRoutes wires every route group and endpoint onto the given engine.
func (h *GatewayHandler) RegisterRoutes(r *gin.Engine, log *zap.Logger) {
	r.GET("/health", h.Health)

	public := r.Group("/api")
	public.Use(ratelimit.RateLimiter(h.rdb, 30))
	{
		public.POST("/register", func(c *gin.Context) {
			h.Register(c, log)
		})
		public.POST("/login", func(c *gin.Context) {
			h.Login(c, log)
		})
		public.POST("/refresh", func(c *gin.Context) {
			h.RefreshToken(c, log)
		})
		public.POST("/logout", func(c *gin.Context) {
			h.Logout(c, log)
		})
	}

	r.GET("/auth/:provider/callback", func(c *gin.Context) {
		h.OAuthCallback(c, log)
	})

	protected := r.Group("/api/v1")
	protected.Use(auth.JWTMiddleware(h.cfg.JWTSecret, h.rdb))
	protected.Use(ratelimit.RateLimiter(h.rdb, 60))
	{
		protected.GET("/me", h.Me)
		protected.POST("/jobs", h.SubmitJob)
		protected.GET("/jobs/:job_id", h.GetJob)
		protected.POST("/jobs/:job_id/approve", h.ApproveJob)
		protected.POST("/jobs/:job_id/reject", h.RejectJob)
		protected.GET("/ws/:job_id", h.HandleWebSocket)
	}

	h.credHandler.RegisterRoutes(protected)
}

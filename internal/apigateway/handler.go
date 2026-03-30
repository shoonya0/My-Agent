package apigateway

import (
	"net/http"

	"myAgent/internal/middleware"
	"myAgent/pkg/model"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// GatewayHandler holds dependencies for all api-gateway HTTP handlers.
type GatewayHandler struct {
	cfg model.Config
	rdb *redis.Client
}

// NewGatewayHandler constructs a GatewayHandler with the required dependencies.
func NewGatewayHandler(cfg model.Config, rdb *redis.Client) *GatewayHandler {
	return &GatewayHandler{
		cfg: cfg,
		rdb: rdb,
	}
}

// RegisterRoutes wires every route group and endpoint onto the given engine.
func (h *GatewayHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)

	public := r.Group("/api")
	public.Use(middleware.RateLimiter(h.rdb, 30))
	{
		public.POST("/register", h.Register)
	}

	protected := r.Group("/api/v1")
	protected.Use(middleware.JWTMiddleware(h.cfg.JWTSecret, h.rdb))
	protected.Use(middleware.RateLimiter(h.rdb, 60))
	{
		protected.GET("/me", h.Me)
		protected.POST("/jobs", h.SubmitJob)
		protected.GET("/jobs/:job_id", h.GetJob)
		protected.POST("/jobs/:job_id/approve", h.ApproveJob)
		protected.POST("/jobs/:job_id/reject", h.RejectJob)
	}
}

// Health is a liveness probe that returns 200 OK.
func (h *GatewayHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Register handles new user registration.
func (h *GatewayHandler) Register(c *gin.Context) {
	// TODO: implement registration logic in auth-service and forward via gRPC
	c.JSON(http.StatusOK, gin.H{"message": "register endpoint"})
}

// Me returns the authenticated user's profile from the JWT claims.
func (h *GatewayHandler) Me(c *gin.Context) {
	user := middleware.CurrentUser(c)
	c.JSON(http.StatusOK, gin.H{
		"user_id": user.UserID,
		"roles":   user.Roles,
		"email":   user.Email,
	})
}

// SubmitJob accepts an image + prompt and kicks off the editing pipeline.
func (h *GatewayHandler) SubmitJob(c *gin.Context) {
	var req model.SubmitJobRequest

	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: upload image to S3, create Job record, publish Kafka event
	c.JSON(http.StatusAccepted, model.SubmitJobResponse{
		JobID:  "", // will be generated
		Status: "pending",
	})
}

// GetJob returns the full detail for a single job.
func (h *GatewayHandler) GetJob(c *gin.Context) {
	jobID := c.Param("job_id")

	// TODO: fetch job from orchestrator / DB
	c.JSON(http.StatusOK, gin.H{
		"job_id": jobID,
		"status": "pending",
	})
}

// ApproveJob marks a generated image as approved and triggers distribution.
func (h *GatewayHandler) ApproveJob(c *gin.Context) {
	jobID := c.Param("job_id")

	var req model.ApproveJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: publish image.approved Kafka event
	c.JSON(http.StatusOK, model.JobActionResponse{
		JobID:   jobID,
		Status:  "distributing",
		Message: "job approved",
	})
}

// RejectJob marks a generated image as rejected.
func (h *GatewayHandler) RejectJob(c *gin.Context) {
	jobID := c.Param("job_id")

	var req model.RejectJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: update job status in DB
	c.JSON(http.StatusOK, model.JobActionResponse{
		JobID:   jobID,
		Status:  "rejected",
		Message: "job rejected",
	})
}

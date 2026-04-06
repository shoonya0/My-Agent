package apigateway

import (
	"net/http"

	"myAgent/api/authpb"
	"myAgent/internal/credentials"
	"myAgent/internal/middleware"
	"myAgent/pkg/messages"
	"myAgent/pkg/model"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxImageSize = 20 << 20 // 20 MB

// GatewayHandler holds dependencies for all api-gateway HTTP handlers.
type GatewayHandler struct {
	cfg         *model.Config
	rdb         *redis.Client
	authClient  authpb.AuthServiceClient
	credHandler *credentials.Handler
}

// NewGatewayHandler constructs a GatewayHandler with the required dependencies.
func NewGatewayHandler(cfg *model.Config, rdb *redis.Client, authClient authpb.AuthServiceClient, credHandler *credentials.Handler) *GatewayHandler {
	return &GatewayHandler{
		cfg:         cfg,
		rdb:         rdb,
		authClient:  authClient,
		credHandler: credHandler,
	}
}

// RegisterRoutes wires every route group and endpoint onto the given engine.
func (h *GatewayHandler) RegisterRoutes(r *gin.Engine, log *zap.Logger) {
	r.GET("/health", h.Health)

	public := r.Group("/api")
	public.Use(middleware.RateLimiter(h.rdb, 30))
	{
		public.POST("/register", func(c *gin.Context) {
			h.Register(c, log)
		})
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

	h.credHandler.RegisterRoutes(protected)
}

// Health is a liveness probe that returns 200 OK.
func (h *GatewayHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type registerGatewayRequest struct {
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=8"`
	DisplayName string `json:"display_name" binding:"required"`
}

// Register handles new user registration by forwarding to auth-service via gRPC.
func (h *GatewayHandler) Register(c *gin.Context, log *zap.Logger) {
	// it binds the request body to the registerGatewayRequest struct
	var req registerGatewayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// ParseBindingErrorWithFields automatically extracts which field(s) are empty/invalid
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	// it makes a gRPC call to the auth-service to register the user
	pbResp, err := h.authClient.Register(c.Request.Context(), &authpb.RegisterUserRequest{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	})
	if err != nil {
		// it checks if the error is an AlreadyExists error
		st, ok := status.FromError(err)
		// if the error is an AlreadyExists error, it returns a 409 Conflict response
		if ok && st.Code() == codes.AlreadyExists {
			c.JSON(http.StatusConflict, messages.ErrorResponse(
				messages.ErrCodeAlreadyExists,
				messages.MsgEmailAlreadyRegistered,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgRegistrationFailed,
		))
		return
	}

	c.JSON(http.StatusCreated, messages.SuccessResponse(
		messages.MsgRegistrationSuccess,
		protoToTokenResponse(pbResp),
	))
}

// Me returns the authenticated user's profile from the JWT claims.
func (h *GatewayHandler) Me(c *gin.Context) {
	user := middleware.CurrentUser(c)
	// it returns the authenticated user's profile from the JWT claims
	c.JSON(http.StatusOK, gin.H{
		"user_id": user.UserID,
		"roles":   user.Roles,
		"email":   user.Email,
	})
}

// SubmitJob accepts an image + prompt and kicks off the editing pipeline.
func (h *GatewayHandler) SubmitJob(c *gin.Context) {
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeMissingField,
			messages.MsgImageRequired,
		))
		return
	}
	defer file.Close()

	if header.Size > maxImageSize {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeFileTooLarge,
			messages.MsgImageTooLarge,
		))
		return
	}

	ct := header.Header.Get("Content-Type")
	if ct != "image/png" && ct != "image/jpeg" && ct != "image/webp" {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeInvalidFileFormat,
			messages.MsgInvalidImageFormat,
		))
		return
	}

	var req model.SubmitJobRequest
	if err := c.ShouldBind(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	// TODO: upload image to S3, create Job record, publish Kafka event
	_ = file // will be read for S3 upload
	resp := model.SubmitJobResponse{
		JobID:  "", // will be generated
		Status: model.JobStatusPending,
	}
	c.JSON(http.StatusAccepted, messages.SuccessResponse(messages.MsgJobSubmitted, resp))
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
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	// TODO: publish image.approved Kafka event
	resp := model.JobActionResponse{
		JobID:   jobID,
		Status:  model.JobStatusDistributing,
		Message: messages.MsgJobApproved,
	}
	c.JSON(http.StatusAccepted, messages.SuccessResponse(messages.MsgJobApproved, resp))
}

// RejectJob marks a generated image as rejected.
func (h *GatewayHandler) RejectJob(c *gin.Context) {
	jobID := c.Param("job_id")

	var req model.RejectJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	// TODO: update job status in DB
	resp := model.JobActionResponse{
		JobID:   jobID,
		Status:  model.JobStatusRejected,
		Message: messages.MsgJobRejected,
	}
	c.JSON(http.StatusOK, messages.SuccessResponse(messages.MsgJobRejected, resp))
}

// ---------------------------------------------------------------------------
// Proto → model converter (gRPC client boundary)
// ---------------------------------------------------------------------------

func protoToTokenResponse(pb *authpb.TokenResponse) *model.TokenResponse {
	return &model.TokenResponse{
		AccessToken:  pb.GetAccessToken(),
		RefreshToken: pb.GetRefreshToken(),
		ExpiresIn:    int(pb.GetExpiresIn()),
		TokenType:    pb.GetTokenType(),
	}
}

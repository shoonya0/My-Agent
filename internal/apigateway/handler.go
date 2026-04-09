package apigateway

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"myAgent/api/authpb"
	"myAgent/api/orchestratorpb"
	"myAgent/internal/credentials"
	"myAgent/internal/middleware"
	"myAgent/pkg/httputil"
	"myAgent/pkg/messages"
	"myAgent/pkg/model"
	"myAgent/pkg/storage"
	ws "myAgent/pkg/websocket"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
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
	orchClient  orchestratorpb.OrchestratorServiceClient
	uploader    storage.Uploader
	credHandler *credentials.Handler
	wsHub       *ws.Hub
}

// NewGatewayHandler constructs a GatewayHandler with the required dependencies.
func NewGatewayHandler(
	cfg *model.Config,
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
	public.Use(middleware.RateLimiter(h.rdb, 30))
	{
		public.POST("/register", func(c *gin.Context) {
			h.Register(c, log)
		})
		public.POST("/login", func(c *gin.Context) {
			h.Login(c, log)
		})
		public.POST("/logout", func(c *gin.Context) {
			h.Logout(c, log)
		})
	}

	r.GET("/auth/:provider/callback", func(c *gin.Context) {
		h.OAuthCallback(c, log)
	})

	protected := r.Group("/api/v1")
	protected.Use(middleware.JWTMiddleware(h.cfg.JWTSecret, h.rdb))
	protected.Use(middleware.RateLimiter(h.rdb, 60))
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

type loginGatewayRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// Login forwards email/password authentication to auth-service via gRPC.
func (h *GatewayHandler) Login(c *gin.Context, log *zap.Logger) {
	var req loginGatewayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	pbResp, err := h.authClient.Login(c.Request.Context(), &authpb.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.Unauthenticated {
			c.JSON(http.StatusUnauthorized, messages.ErrorResponse(
				messages.ErrCodeInvalidCredentials,
				messages.MsgInvalidCredentials,
			))
			return
		}
		log.Error("Login gRPC call failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgLoginFailed,
		))
		return
	}

	c.JSON(http.StatusOK, messages.SuccessResponse(
		messages.MsgLoginSuccess,
		protoToTokenResponse(pbResp),
	))
}

// Logout revokes the current access token via auth-service gRPC.
func (h *GatewayHandler) Logout(c *gin.Context, log *zap.Logger) {
	token, err := httputil.ExtractBearerToken(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = h.authClient.Logout(c.Request.Context(), &authpb.LogoutRequest{
		Token: token,
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.InvalidArgument {
			c.JSON(http.StatusBadRequest, messages.ErrorResponse(
				messages.ErrCodeTokenInvalid,
				messages.MsgTokenInvalid,
			))
			return
		}
		log.Error("Logout gRPC call failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgLogoutFailed,
		))
		return
	}

	c.JSON(http.StatusOK, messages.SuccessResponse(messages.MsgLogoutSuccess, nil))
}

// OAuthCallback completes the OAuth2 authorization code flow via auth-service gRPC.
func (h *GatewayHandler) OAuthCallback(c *gin.Context, log *zap.Logger) {
	provider := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeMissingField,
			"OAuth code and state are required",
		))
		return
	}

	pbResp, err := h.authClient.HandleOAuthCallback(c.Request.Context(), &authpb.OAuthCallbackRequest{
		Provider: provider,
		Code:     code,
		State:    state,
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.InvalidArgument:
				c.JSON(http.StatusBadRequest, messages.ErrorResponse(
					messages.ErrCodeInvalidInput,
					messages.MsgInvalidOAuthState,
				))
				return
			case codes.Unauthenticated:
				c.JSON(http.StatusUnauthorized, messages.ErrorResponse(
					messages.ErrCodeUnauthorized,
					messages.MsgOAuthFailed,
				))
				return
			case codes.FailedPrecondition:
				c.JSON(http.StatusServiceUnavailable, messages.ErrorResponse(
					messages.ErrCodeServiceUnavailable,
					messages.MsgOAuthNotConfigured,
				))
				return
			case codes.AlreadyExists:
				c.JSON(http.StatusConflict, messages.ErrorResponse(
					messages.ErrCodeAlreadyExists,
					messages.MsgEmailAlreadyRegistered,
				))
				return
			}
		}
		log.Error("OAuth callback gRPC call failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgOAuthFailed,
		))
		return
	}

	c.JSON(http.StatusOK, messages.SuccessResponse(
		messages.MsgLoginSuccess,
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

	if len(req.Platforms) == 0 {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeMissingField,
			messages.MsgPlatformsRequired,
		))
		return
	}

	body, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeInvalidInput,
			"failed to read image upload",
		))
		return
	}

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		switch ct {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/webp":
			ext = ".webp"
		}
	}

	user := middleware.CurrentUser(c)
	key := fmt.Sprintf("original/%s/%s%s", user.UserID, uuid.New().String(), ext)
	imageURL, err := h.uploader.Upload(c.Request.Context(), key, body, ct)
	if err != nil {
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			"failed to store image",
		))
		return
	}

	pbResp, err := h.orchClient.SubmitJob(c.Request.Context(), &orchestratorpb.SubmitJobRequest{
		UserId:    user.UserID,
		Prompt:    req.Prompt,
		ImageUrl:  imageURL,
		Platforms: req.Platforms,
		Caption:   req.Caption,
	})
	if err != nil {
		if writeOrchestratorGRPCError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgJobSubmissionFailed,
		))
		return
	}

	resp := model.SubmitJobResponse{
		JobID:     pbResp.GetJobId(),
		Status:    pbResp.GetStatus(),
		WsURL:     pbResp.GetWsUrl(),
		CreatedAt: time.Unix(pbResp.GetCreatedAtUnix(), 0).UTC(),
	}
	c.JSON(http.StatusAccepted, messages.SuccessResponse(messages.MsgJobSubmitted, resp))
}

// GetJob returns the full detail for a single job.
func (h *GatewayHandler) GetJob(c *gin.Context) {
	jobID := c.Param("job_id")
	user := middleware.CurrentUser(c)

	pbResp, err := h.orchClient.GetJob(c.Request.Context(), &orchestratorpb.GetJobRequest{
		JobId:  jobID,
		UserId: user.UserID,
	})
	if err != nil {
		if writeOrchestratorGRPCError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgInternalServerError,
		))
		return
	}

	out := model.GetJobResponse{
		ID:                pbResp.GetId(),
		Status:            pbResp.GetStatus(),
		OriginalPrompt:    pbResp.GetOriginalPrompt(),
		RefinedPrompt:     pbResp.GetRefinedPrompt(),
		OriginalImageURL:  pbResp.GetOriginalImageUrl(),
		GeneratedImageURL: pbResp.GetGeneratedImageUrl(),
		CreatedAt:         time.Unix(pbResp.GetCreatedAtUnix(), 0).UTC(),
	}
	for _, pr := range pbResp.GetPostResults() {
		out.PostResults = append(out.PostResults, model.PostResult{
			ID:             pr.GetId(),
			JobID:          pr.GetJobId(),
			UserID:         pr.GetUserId(),
			Platform:       pr.GetPlatform(),
			Status:         pr.GetStatus(),
			PlatformPostID: pr.GetPlatformPostId(),
			PlatformURL:    pr.GetPlatformUrl(),
			ErrorDetail:    pr.GetErrorDetail(),
			AttemptCount:   int(pr.GetAttemptCount()),
			CreatedAt:      time.Unix(pr.GetCreatedAtUnix(), 0).UTC(),
		})
	}

	c.JSON(http.StatusOK, messages.SuccessResponse("Job retrieved", out))
}

// ApproveJob marks a generated image as approved and triggers distribution.
func (h *GatewayHandler) ApproveJob(c *gin.Context) {
	jobID := c.Param("job_id")
	user := middleware.CurrentUser(c)

	var req model.ApproveJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	if len(req.Platforms) == 0 {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeMissingField,
			messages.MsgPlatformsRequired,
		))
		return
	}

	pbResp, err := h.orchClient.ApproveJob(c.Request.Context(), &orchestratorpb.ApproveJobRequest{
		JobId:     jobID,
		UserId:    user.UserID,
		Caption:   req.Caption,
		Platforms: req.Platforms,
	})
	if err != nil {
		if writeOrchestratorGRPCError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgJobApprovalFailed,
		))
		return
	}

	resp := model.JobActionResponse{
		JobID:   pbResp.GetJobId(),
		Status:  pbResp.GetStatus(),
		Message: pbResp.GetMessage(),
	}
	c.JSON(http.StatusAccepted, messages.SuccessResponse(messages.MsgJobApproved, resp))
}

// RejectJob marks a generated image as rejected.
func (h *GatewayHandler) RejectJob(c *gin.Context) {
	jobID := c.Param("job_id")
	user := middleware.CurrentUser(c)

	var req model.RejectJobRequest
	_ = c.ShouldBindJSON(&req)

	pbResp, err := h.orchClient.RejectJob(c.Request.Context(), &orchestratorpb.RejectJobRequest{
		JobId:  jobID,
		UserId: user.UserID,
		Reason: req.Reason,
	})
	if err != nil {
		if writeOrchestratorGRPCError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgJobRejectionFailed,
		))
		return
	}

	resp := model.JobActionResponse{
		JobID:   pbResp.GetJobId(),
		Status:  pbResp.GetStatus(),
		Message: pbResp.GetMessage(),
	}
	c.JSON(http.StatusOK, messages.SuccessResponse(messages.MsgJobRejected, resp))
}

// writeOrchestratorGRPCError maps orchestrator gRPC status codes to HTTP responses.
// It returns true when err was handled.
func writeOrchestratorGRPCError(c *gin.Context, err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.NotFound:
		c.JSON(http.StatusNotFound, messages.ErrorResponse(
			messages.ErrCodeJobNotFound,
			messages.MsgJobNotFound,
		))
	case codes.PermissionDenied:
		c.JSON(http.StatusForbidden, messages.ErrorResponse(
			messages.ErrCodeUnauthorized,
			messages.MsgUnauthorized,
		))
	case codes.InvalidArgument:
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeInvalidInput,
			st.Message(),
		))
	case codes.FailedPrecondition:
		c.JSON(http.StatusConflict, messages.ErrorResponse(
			messages.ErrCodeOperationFailed,
			st.Message(),
		))
	default:
		return false
	}
	return true
}

// HandleWebSocket upgrades the HTTP connection to a WebSocket for real-time
// job update notifications. Authentication is accepted via the Authorization
// header or a ?token= query parameter (for browser clients).
func (h *GatewayHandler) HandleWebSocket(c *gin.Context) {
	jobID := c.Param("job_id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job_id is required"})
		return
	}

	user, err := h.authenticateWS(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	conn, err := ws.Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "WebSocket upgrade failed"})
		return
	}

	client := ws.NewClient(h.wsHub, conn, jobID, user.UserID)
	h.wsHub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}

// authenticateWS validates JWT from the Authorization header or the ?token=
// query parameter. It mirrors the logic in middleware.JWTMiddleware but
// supports the query-parameter flow required by browser WebSocket clients.
func (h *GatewayHandler) authenticateWS(c *gin.Context) (*model.AuthenticatedUser, error) {
	tokenStr := ""

	if auth := c.GetHeader("Authorization"); auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			tokenStr = strings.TrimSpace(parts[1])
		}
	}

	if tokenStr == "" {
		tokenStr = c.Query("token")
	}

	if tokenStr == "" {
		return nil, errors.New("authentication required")
	}

	token, err := jwt.ParseWithClaims(tokenStr, &middleware.CustomClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(h.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, errors.New("invalid or expired token")
	}

	claims, ok := token.Claims.(*middleware.CustomClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	if claims.ID != "" && h.rdb != nil {
		exists, rErr := h.rdb.Exists(c.Request.Context(), "jwt:blacklist:"+claims.ID).Result()
		if rErr == nil && exists > 0 {
			return nil, errors.New("token has been revoked")
		}
	}

	return &model.AuthenticatedUser{
		UserID: claims.UserID,
		Roles:  claims.Roles,
		Email:  claims.Email,
	}, nil
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

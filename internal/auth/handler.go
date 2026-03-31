package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"myAgent/api/authpb"
	"myAgent/pkg/grpcserver"
	"myAgent/pkg/model"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Handler holds dependencies for auth HTTP and gRPC endpoints.
type Handler struct {
	svc Service
	log *zap.Logger
}

// NewHandler constructs an auth Handler with the required dependencies.
func NewHandler(svc Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// GRPCRegistrar returns a grpcserver.Registrar that registers the
// auth.v1.AuthService gRPC implementation onto the given server.
func (h *Handler) GRPCRegistrar() grpcserver.Registrar {
	return func(srv *grpc.Server) {
		authpb.RegisterAuthServiceServer(srv, h)
	}
}

// ValidateToken implements authpb.AuthServiceServer. Called by api-gateway
// on every authenticated request to verify the JWT.
func (h *Handler) ValidateToken(ctx context.Context, req *model.ValidateTokenRequest) (*model.Claims, error) {
	claims, err := h.svc.ValidateToken(ctx, req.Token)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "%v", err)
	}
	return claims, nil
}

// Register implements authpb.AuthServiceServer. Called by api-gateway to
// create a new user account and return a token pair.
func (h *Handler) Register(ctx context.Context, req *model.RegisterUserRequest) (*model.TokenResponse, error) {
	resp, err := h.svc.Register(ctx, RegisterRequest{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	})
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return nil, status.Errorf(codes.AlreadyExists, "email already registered")
		}
		h.log.Error("Register gRPC failed", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "registration failed")
	}
	return resp, nil
}

// RegisterRoutes attaches auth HTTP endpoints to the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.health)

	auth := r.Group("/auth")
	{
		auth.POST("/register", h.register)
		auth.POST("/login", h.login)
		auth.POST("/logout", h.logout)
	}

	// TODO: OAuth2 provider callback routes
	// auth.GET("/:provider/callback", h.oauthCallback)
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

type registerHTTPRequest struct {
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=8"`
	DisplayName string `json:"display_name" binding:"required"`
}

type loginHTTPRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) register(c *gin.Context) {
	var req registerHTTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.svc.Register(c.Request.Context(), RegisterRequest{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	})
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

func (h *Handler) login(c *gin.Context) {
	var req loginHTTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.svc.Login(c.Request.Context(), LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) logout(c *gin.Context) {
	token := extractBearerToken(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bearer token required"})
		return
	}

	if err := h.svc.RevokeToken(c.Request.Context(), token); err != nil {
		h.log.Error("Failed to revoke token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}

func (h *Handler) handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
	case errors.Is(err, ErrEmailTaken):
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
	default:
		h.log.Error("Internal error", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

func extractBearerToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	parts := strings.SplitN(header, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

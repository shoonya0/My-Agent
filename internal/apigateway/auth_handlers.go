package apigateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"myAgent/api/authpb"
	"myAgent/pkg/httputil"
	"myAgent/pkg/messages"
	"myAgent/pkg/middleware/auth"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
	var req registerGatewayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	pbResp, err := h.authClient.Register(c.Request.Context(), &authpb.RegisterUserRequest{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	})
	if err != nil {
		st, ok := status.FromError(err)
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

type logoutGatewayRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Logout revokes the current access token via auth-service gRPC and optionally the refresh token from the JSON body.
func (h *GatewayHandler) Logout(c *gin.Context, log *zap.Logger) {
	token, err := httputil.ExtractBearerToken(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var body logoutGatewayRequest
	if c.Request.Body != nil {
		dec := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
		if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, messages.ErrorResponse(
				messages.ErrCodeInvalidInput,
				"Invalid JSON body",
			))
			return
		}
	}

	_, err = h.authClient.Logout(c.Request.Context(), &authpb.LogoutRequest{
		Token:        token,
		RefreshToken: strings.TrimSpace(body.RefreshToken),
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

// RefreshToken handles token refresh requests by forwarding to auth-service via gRPC.
func (h *GatewayHandler) RefreshToken(c *gin.Context, log *zap.Logger) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	pbResp, err := h.authClient.RefreshToken(c.Request.Context(), &authpb.RefreshTokenRequest{
		RefreshToken: req.RefreshToken,
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.Unauthenticated {
			c.JSON(http.StatusUnauthorized, messages.ErrorResponse(
				messages.ErrCodeUnauthorized,
				"Invalid or expired refresh token",
			))
			return
		}
		log.Error("RefreshToken gRPC call failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			"Token refresh failed",
		))
		return
	}

	c.JSON(http.StatusOK, messages.SuccessResponse(
		"Token refreshed successfully",
		protoToTokenResponse(pbResp),
	))
}

// Me returns the authenticated user's profile from the JWT claims.
func (h *GatewayHandler) Me(c *gin.Context) {
	user := auth.CurrentUser(c)
	c.JSON(http.StatusOK, gin.H{
		"user_id": user.UserID,
		"roles":   user.Roles,
		"email":   user.Email,
	})
}

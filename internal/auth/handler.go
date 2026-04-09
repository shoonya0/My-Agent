package auth

import (
	"context"
	"errors"

	"myAgent/api/authpb"
	"myAgent/pkg/grpcserver"
	"myAgent/pkg/model"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Handler implements authpb.AuthServiceServer (gRPC only; HTTP is served by api-gateway).
type Handler struct {
	authpb.UnimplementedAuthServiceServer
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

// ---------------------------------------------------------------------------
// gRPC handlers — thin converters between proto types and domain types
// ---------------------------------------------------------------------------

// ValidateToken implements authpb.AuthServiceServer.
func (h *Handler) ValidateToken(ctx context.Context, req *authpb.ValidateTokenRequest) (*authpb.ValidateTokenResponse, error) {
	claims, err := h.svc.ValidateToken(ctx, req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "%v", err)
	}
	return claimsToProto(claims), nil
}

// Register implements authpb.AuthServiceServer.
func (h *Handler) Register(ctx context.Context, req *authpb.RegisterUserRequest) (*authpb.TokenResponse, error) {
	resp, err := h.svc.Register(ctx, RegisterRequest{
		Email:       req.GetEmail(),
		Password:    req.GetPassword(),
		DisplayName: req.GetDisplayName(),
	})
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return nil, status.Errorf(codes.AlreadyExists, "email already registered")
		}
		h.log.Error("Register gRPC failed", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "registration failed")
	}
	return tokenResponseToProto(resp), nil
}

// Login implements authpb.AuthServiceServer.
func (h *Handler) Login(ctx context.Context, req *authpb.LoginRequest) (*authpb.TokenResponse, error) {
	resp, err := h.svc.Login(ctx, LoginRequest{
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	})
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return nil, status.Errorf(codes.Unauthenticated, "invalid credentials")
		}
		h.log.Error("Login gRPC failed", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "login failed")
	}
	return tokenResponseToProto(resp), nil
}

// Logout implements authpb.AuthServiceServer.
func (h *Handler) Logout(ctx context.Context, req *authpb.LogoutRequest) (*authpb.LogoutResponse, error) {
	if err := h.svc.RevokeToken(ctx, req.GetToken()); err != nil {
		switch {
		case errors.Is(err, ErrInvalidLogoutToken),
			errors.Is(err, ErrLogoutTokenMissingJTI),
			errors.Is(err, ErrLogoutTokenMissingExpiry):
			return nil, status.Errorf(codes.InvalidArgument, "invalid token")
		}
		h.log.Error("Logout gRPC failed", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "logout failed")
	}
	return &authpb.LogoutResponse{Success: true}, nil
}

// HandleOAuthCallback implements authpb.AuthServiceServer.
func (h *Handler) HandleOAuthCallback(ctx context.Context, req *authpb.OAuthCallbackRequest) (*authpb.TokenResponse, error) {
	resp, err := h.svc.HandleOAuthCallback(ctx, OAuthCallbackParams{
		Provider: req.GetProvider(),
		Code:     req.GetCode(),
		State:    req.GetState(),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidOAuthState):
			return nil, status.Errorf(codes.InvalidArgument, "invalid or expired OAuth state")
		case errors.Is(err, ErrOAuthProviderUnknown):
			return nil, status.Errorf(codes.InvalidArgument, "unknown OAuth provider")
		case errors.Is(err, ErrOAuthNotConfigured):
			return nil, status.Errorf(codes.FailedPrecondition, "OAuth provider not configured")
		case errors.Is(err, ErrOAuthExchangeFailed):
			return nil, status.Errorf(codes.Unauthenticated, "OAuth code exchange failed")
		case errors.Is(err, ErrOAuthProfileFailed):
			return nil, status.Errorf(codes.Unauthenticated, "OAuth profile fetch failed")
		case errors.Is(err, ErrOAuthEmailConflict):
			return nil, status.Errorf(codes.AlreadyExists, "email already registered with another account")
		}
		h.log.Error("OAuth callback gRPC failed", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "OAuth failed")
	}
	return tokenResponseToProto(resp), nil
}

// ---------------------------------------------------------------------------
// Proto ↔ model converters (gRPC boundary only)
// ---------------------------------------------------------------------------

func claimsToProto(c *model.Claims) *authpb.ValidateTokenResponse {
	return &authpb.ValidateTokenResponse{
		UserId:    c.UserID,
		Roles:     c.Roles,
		ExpiresAt: c.ExpiresAt,
	}
}

func tokenResponseToProto(r *model.TokenResponse) *authpb.TokenResponse {
	return &authpb.TokenResponse{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		ExpiresIn:    int32(r.ExpiresIn),
		TokenType:    r.TokenType,
	}
}

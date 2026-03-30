package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"myAgent/pkg/model"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
	bcryptCost      = 12
)

var (
	// ErrInvalidCredentials is returned when email/password do not match.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrEmailTaken is returned when a registration email is already in use.
	ErrEmailTaken = errors.New("auth: email already registered")
)

// Service defines the business logic for authentication and authorization.
type Service interface {
	Register(ctx context.Context, req RegisterRequest) (*model.TokenResponse, error)
	Login(ctx context.Context, req LoginRequest) (*model.TokenResponse, error)
	ValidateToken(ctx context.Context, token string) (*model.Claims, error)
	RevokeToken(ctx context.Context, token string) error
}

// RegisterRequest is the input for email/password registration.
type RegisterRequest struct {
	Email       string
	Password    string
	DisplayName string
}

// LoginRequest is the input for email/password authentication.
type LoginRequest struct {
	Email    string
	Password string
}

type authService struct {
	repo      Repository
	rdb       *redis.Client
	jwtSecret []byte
	log       *zap.Logger
}

// NewService constructs an auth Service with the required dependencies.
func NewService(repo Repository, rdb *redis.Client, jwtSecret string, log *zap.Logger) Service {
	return &authService{
		repo:      repo,
		rdb:       rdb,
		jwtSecret: []byte(jwtSecret),
		log:       log,
	}
}

// jwtClaims mirrors middleware.CustomClaims — identical JSON tags ensure tokens
// issued here are accepted by the api-gateway's JWT middleware.
type jwtClaims struct {
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
	Email  string   `json:"email"`
	jwt.RegisteredClaims
}

func (s *authService) Register(ctx context.Context, req RegisterRequest) (*model.TokenResponse, error) {
	existing, err := s.repo.GetUserByEmail(ctx, req.Email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, fmt.Errorf("auth: check existing user: %w", err)
	}
	if existing != nil {
		return nil, ErrEmailTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}

	user := &model.User{
		ID:           uuid.NewString(),
		Email:        req.Email,
		PasswordHash: string(hash),
		DisplayName:  req.DisplayName,
		Provider:     "local",
		Roles:        []string{"user"},
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("auth: create user: %w", err)
	}

	s.log.Info("User registered", zap.String("user_id", user.ID), zap.String("email", user.Email))
	return s.issueTokenPair(user)
}

func (s *authService) Login(ctx context.Context, req LoginRequest) (*model.TokenResponse, error) {
	user, err := s.repo.GetUserByEmail(ctx, req.Email)
	if errors.Is(err, ErrUserNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("auth: get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	s.log.Info("User logged in", zap.String("user_id", user.ID))
	return s.issueTokenPair(user)
}

func (s *authService) ValidateToken(ctx context.Context, tokenStr string) (*model.Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &jwtClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: parse token: %w", err)
	}

	claims, ok := token.Claims.(*jwtClaims)
	if !ok || !token.Valid {
		return nil, errors.New("auth: invalid token claims")
	}

	if claims.ID != "" {
		exists, err := s.rdb.Exists(ctx, "jwt:blacklist:"+claims.ID).Result()
		if err != nil {
			s.log.Warn("Redis blacklist check failed, allowing token", zap.Error(err))
		} else if exists > 0 {
			return nil, errors.New("auth: token revoked")
		}
	}

	var expiresAt int64
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Unix()
	}

	return &model.Claims{
		UserID:    claims.UserID,
		Roles:     claims.Roles,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *authService) RevokeToken(ctx context.Context, tokenStr string) error {
	token, err := jwt.ParseWithClaims(tokenStr, &jwtClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return fmt.Errorf("auth: parse token for revocation: %w", err)
	}

	claims, ok := token.Claims.(*jwtClaims)
	if !ok || claims.ID == "" {
		return errors.New("auth: token has no JTI")
	}

	if claims.ExpiresAt == nil {
		return errors.New("auth: token has no expiry")
	}

	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl <= 0 {
		return nil
	}

	key := "jwt:blacklist:" + claims.ID
	if err := s.rdb.Set(ctx, key, "1", ttl).Err(); err != nil {
		return fmt.Errorf("auth: blacklist token: %w", err)
	}

	s.log.Info("Token revoked", zap.String("jti", claims.ID))
	return nil
}

func (s *authService) issueTokenPair(user *model.User) (*model.TokenResponse, error) {
	now := time.Now()

	accessClaims := jwtClaims{
		UserID: user.ID,
		Roles:  user.Roles,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
		},
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("auth: sign access token: %w", err)
	}

	refreshClaims := jwt.RegisteredClaims{
		ID:        uuid.NewString(),
		Subject:   user.ID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(refreshTokenTTL)),
	}

	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("auth: sign refresh token: %w", err)
	}

	return &model.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

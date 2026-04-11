package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"myAgent/pkg/types"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
	bcryptCost      = 12
)

const tracerName = "internal/auth"

var (
	// ErrInvalidCredentials is returned when email/password do not match.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrEmailTaken is returned when a registration email is already in use.
	ErrEmailTaken = errors.New("auth: email already registered")
	// ErrInvalidLogoutToken is returned when the bearer token cannot be parsed for revocation.
	ErrInvalidLogoutToken = errors.New("auth: invalid token for logout")
	// ErrLogoutTokenMissingJTI is returned when the token has no JTI for blacklisting.
	ErrLogoutTokenMissingJTI = errors.New("auth: token has no JTI")
	// ErrLogoutTokenMissingExpiry is returned when the token has no expiry.
	ErrLogoutTokenMissingExpiry = errors.New("auth: token has no expiry")
	// ErrInvalidOAuthState is returned when the OAuth CSRF state is missing or already used.
	ErrInvalidOAuthState = errors.New("auth: invalid or expired OAuth state")
	// ErrOAuthProviderUnknown is returned for unsupported provider names.
	ErrOAuthProviderUnknown = errors.New("auth: unknown OAuth provider")
	// ErrOAuthNotConfigured is returned when provider OAuth client credentials are not set.
	ErrOAuthNotConfigured = errors.New("auth: OAuth provider not configured")
	// ErrOAuthExchangeFailed is returned when the authorization code exchange fails.
	ErrOAuthExchangeFailed = errors.New("auth: OAuth code exchange failed")
	// ErrOAuthProfileFailed is returned when the provider userinfo request fails.
	ErrOAuthProfileFailed = errors.New("auth: OAuth profile fetch failed")
	// ErrOAuthEmailConflict is returned when the OAuth email is already registered to another account.
	ErrOAuthEmailConflict = errors.New("auth: OAuth email already in use")
	// ErrInvalidRefreshToken is returned when the refresh token is invalid or expired.
	ErrInvalidRefreshToken = errors.New("auth: invalid or expired refresh token")
	// ErrRefreshTokenRevoked is returned when the refresh token has been revoked.
	ErrRefreshTokenRevoked = errors.New("auth: refresh token has been revoked")
)

var googleOAuthEndpoint = oauth2.Endpoint{
	AuthURL:  "https://accounts.google.com/o/oauth2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
}

var githubEndpoint = oauth2.Endpoint{
	AuthURL:  "https://github.com/login/oauth/authorize",
	TokenURL: "https://github.com/login/oauth/access_token",
}

// Service defines the business logic for authentication and authorization.
type Service interface {
	Register(ctx context.Context, req RegisterRequest) (*types.TokenResponse, error)
	Login(ctx context.Context, req LoginRequest) (*types.TokenResponse, error)
	ValidateToken(ctx context.Context, token string) (*types.Claims, error)
	Logout(ctx context.Context, accessToken, refreshToken string) error
	HandleOAuthCallback(ctx context.Context, req OAuthCallbackParams) (*types.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*types.TokenResponse, error)
}

// OAuthCallbackParams is the domain input for completing an OAuth2 authorization code callback.
type OAuthCallbackParams struct {
	Provider string
	Code     string
	State    string
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
	cfg       *types.Config
	log       *zap.Logger
}

// NewService constructs an auth Service with the required dependencies.
func NewService(repo Repository, rdb *redis.Client, cfg *types.Config, log *zap.Logger) Service {
	return &authService{
		repo:      repo,
		rdb:       rdb,
		jwtSecret: []byte(cfg.JWTSecret),
		cfg:       cfg,
		log:       log,
	}
}

// jwtClaims mirrors auth.CustomClaims — identical JSON tags ensure tokens
// issued here are accepted by the api-gateway's JWT middleware.
type jwtClaims struct {
	UserID   string   `json:"user_id"`
	Roles    []string `json:"roles"`
	Email    string   `json:"email"`
	TokenUse string   `json:"token_use"`
	jwt.RegisteredClaims
}

func (s *authService) Register(ctx context.Context, req RegisterRequest) (*types.TokenResponse, error) {
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

	user := &types.User{
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

	return s.issueTokenPair(user)
}

func (s *authService) Login(ctx context.Context, req LoginRequest) (*types.TokenResponse, error) {
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

func (s *authService) ValidateToken(ctx context.Context, tokenStr string) (*types.Claims, error) {
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

	// token_use distinguishes access vs refresh; only access tokens may call ValidateToken.
	if claims.TokenUse == "refresh" {
		return nil, errors.New("auth: refresh token cannot be used as an access token")
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

	return &types.Claims{
		UserID:    claims.UserID,
		Roles:     claims.Roles,
		ExpiresAt: expiresAt,
	}, nil
}

// Logout blacklists the access token JTI (required). When refreshToken is non-empty, it also
// blacklists that refresh token JTI so the session cannot be extended after sign-out.
func (s *authService) Logout(ctx context.Context, accessToken, refreshToken string) error {
	if err := s.revokeAccessToken(ctx, accessToken); err != nil {
		return err
	}
	rt := strings.TrimSpace(refreshToken)
	if rt == "" {
		return nil
	}
	if err := s.revokeRefreshToken(ctx, rt); err != nil {
		s.log.Warn("Refresh token could not be revoked during logout", zap.Error(err))
	}
	return nil
}

func (s *authService) revokeAccessToken(ctx context.Context, tokenStr string) error {
	token, err := jwt.ParseWithClaims(tokenStr, &jwtClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidLogoutToken, err)
	}

	claims, ok := token.Claims.(*jwtClaims)
	if !ok || claims.ID == "" {
		return ErrLogoutTokenMissingJTI
	}

	if claims.TokenUse == "refresh" {
		return fmt.Errorf("%w: bearer must be an access token", ErrInvalidLogoutToken)
	}

	if claims.ExpiresAt == nil {
		return ErrLogoutTokenMissingExpiry
	}

	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl <= 0 {
		return nil
	}

	key := "jwt:blacklist:" + claims.ID
	if err := s.rdb.Set(ctx, key, "1", ttl).Err(); err != nil {
		return fmt.Errorf("auth: blacklist access token: %w", err)
	}

	s.log.Info("Access token revoked", zap.String("jti", claims.ID))
	return nil
}

// revokeRefreshToken blacklists a refresh token by JTI. Only tokens with token_use=refresh are accepted.
func (s *authService) revokeRefreshToken(ctx context.Context, tokenStr string) error {
	token, err := jwt.ParseWithClaims(tokenStr, &jwtClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return fmt.Errorf("refresh: %w", err)
	}

	claims, ok := token.Claims.(*jwtClaims)
	if !ok || claims.ID == "" {
		return ErrLogoutTokenMissingJTI
	}

	if claims.TokenUse != "refresh" {
		return fmt.Errorf("refresh: expected refresh token")
	}

	if claims.ExpiresAt == nil {
		return ErrLogoutTokenMissingExpiry
	}

	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl <= 0 {
		return nil
	}

	key := "jwt:blacklist:" + claims.ID
	if err := s.rdb.Set(ctx, key, "1", ttl).Err(); err != nil {
		return fmt.Errorf("auth: blacklist refresh token: %w", err)
	}

	s.log.Info("Refresh token revoked", zap.String("jti", claims.ID))
	return nil
}

func (s *authService) HandleOAuthCallback(ctx context.Context, req OAuthCallbackParams) (*types.TokenResponse, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "auth.HandleOAuthCallback")
	defer span.End()

	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	code := strings.TrimSpace(req.Code)
	state := strings.TrimSpace(req.State)
	span.SetAttributes(attribute.String("auth.oauth.provider", provider))

	if code == "" || state == "" {
		span.SetStatus(otelcodes.Error, "missing code or state")
		return nil, fmt.Errorf("%w", ErrInvalidOAuthState)
	}

	ocfg := s.oauth2Config(provider)
	if ocfg == nil {
		if provider != "google" && provider != "github" {
			span.SetStatus(otelcodes.Error, "unknown provider")
			return nil, ErrOAuthProviderUnknown
		}
		span.SetStatus(otelcodes.Error, "not configured")
		return nil, ErrOAuthNotConfigured
	}

	stateKey := "oauth:state:" + state
	_, err := s.rdb.GetDel(ctx, stateKey).Result()
	if errors.Is(err, redis.Nil) {
		span.SetStatus(otelcodes.Error, "invalid state")
		return nil, ErrInvalidOAuthState
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "redis state")
		return nil, fmt.Errorf("auth: oauth state redis: %w", err)
	}

	ctx, exSpan := otel.Tracer(tracerName).Start(ctx, "auth.OAuthExchange")
	tok, err := ocfg.Exchange(ctx, code)
	if err != nil {
		exSpan.RecordError(err)
		exSpan.SetStatus(otelcodes.Error, "exchange failed")
		exSpan.End()
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "exchange failed")
		return nil, fmt.Errorf("%w: %v", ErrOAuthExchangeFailed, err)
	}
	exSpan.End()

	httpClient := ocfg.Client(ctx, tok)
	prof, err := s.fetchOAuthProfile(ctx, provider, httpClient)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "profile")
		return nil, err
	}

	user, err := s.repo.GetUserByProviderID(ctx, provider, prof.ProviderUserID)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, fmt.Errorf("auth: lookup oauth user: %w", err)
	}

	if user != nil {
		user.DisplayName = prof.DisplayName
		user.AvatarURL = prof.AvatarURL
		if prof.Email != "" {
			user.Email = prof.Email
		}
		if err := s.repo.UpdateUser(ctx, user); err != nil {
			return nil, fmt.Errorf("auth: update oauth user: %w", err)
		}
		s.log.Info("OAuth user signed in", zap.String("user_id", user.ID), zap.String("provider", provider))
		return s.issueTokenPair(user)
	}

	if prof.Email == "" {
		return nil, fmt.Errorf("%w: provider did not return an email", ErrOAuthProfileFailed)
	}

	newUser := &types.User{
		ID:          uuid.NewString(),
		Email:       prof.Email,
		DisplayName: prof.DisplayName,
		AvatarURL:   prof.AvatarURL,
		Provider:    provider,
		ProviderID:  prof.ProviderUserID,
		Roles:       []string{"user"},
	}

	if err := s.repo.CreateUser(ctx, newUser); err != nil {
		var myErr *mysqldriver.MySQLError
		if errors.As(err, &myErr) && myErr.Number == 1062 {
			return nil, ErrOAuthEmailConflict
		}
		return nil, fmt.Errorf("auth: create oauth user: %w", err)
	}

	s.log.Info("OAuth user registered", zap.String("user_id", newUser.ID), zap.String("provider", provider))
	return s.issueTokenPair(newUser)
}

type oauthProfile struct {
	ProviderUserID string
	Email          string
	DisplayName    string
	AvatarURL      string
}

func (s *authService) oauth2Config(provider string) *oauth2.Config {
	switch provider {
	case "google":
		if s.cfg.GoogleOAuthClientID == "" || s.cfg.GoogleOAuthClientSecret == "" || s.cfg.GoogleOAuthRedirectURL == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     s.cfg.GoogleOAuthClientID,
			ClientSecret: s.cfg.GoogleOAuthClientSecret,
			RedirectURL:  s.cfg.GoogleOAuthRedirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     googleOAuthEndpoint,
		}
	case "github":
		if s.cfg.GithubOAuthClientID == "" || s.cfg.GithubOAuthClientSecret == "" || s.cfg.GithubOAuthRedirectURL == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     s.cfg.GithubOAuthClientID,
			ClientSecret: s.cfg.GithubOAuthClientSecret,
			RedirectURL:  s.cfg.GithubOAuthRedirectURL,
			Scopes:       []string{"read:user", "user:email"},
			Endpoint:     githubEndpoint,
		}
	default:
		return nil
	}
}

func (s *authService) fetchOAuthProfile(ctx context.Context, provider string, client *http.Client) (*oauthProfile, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "auth.OAuthUserInfo")
	defer span.End()

	switch provider {
	case "google":
		return s.fetchGoogleProfile(ctx, client)
	case "github":
		return s.fetchGitHubProfile(ctx, client)
	default:
		return nil, ErrOAuthProviderUnknown
	}
}

func (s *authService) fetchGoogleProfile(ctx context.Context, client *http.Client) (*oauthProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrOAuthProfileFailed, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d: %s", ErrOAuthProfileFailed, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var p struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	if p.ID == "" {
		return nil, fmt.Errorf("%w: missing user id", ErrOAuthProfileFailed)
	}
	return &oauthProfile{
		ProviderUserID: p.ID,
		Email:          p.Email,
		DisplayName:    p.Name,
		AvatarURL:      p.Picture,
	}, nil
}

func (s *authService) fetchGitHubProfile(ctx context.Context, client *http.Client) (*oauthProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrOAuthProfileFailed, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d: %s", ErrOAuthProfileFailed, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		Email     string `json:"email"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	email := strings.TrimSpace(u.Email)
	if email == "" {
		e, err := s.fetchGitHubPrimaryEmail(ctx, client)
		if err != nil {
			return nil, err
		}
		email = e
	}
	display := strings.TrimSpace(u.Name)
	if display == "" {
		display = u.Login
	}
	return &oauthProfile{
		ProviderUserID: fmt.Sprintf("%d", u.ID),
		Email:          email,
		DisplayName:    display,
		AvatarURL:      u.AvatarURL,
	}, nil
}

func (s *authService) fetchGitHubPrimaryEmail(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("%w: read body: %v", ErrOAuthProfileFailed, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d: %s", ErrOAuthProfileFailed, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var entries []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return "", fmt.Errorf("%w: %v", ErrOAuthProfileFailed, err)
	}
	for _, e := range entries {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	for _, e := range entries {
		if e.Verified {
			return e.Email, nil
		}
	}
	return "", fmt.Errorf("%w: no verified email", ErrOAuthProfileFailed)
}

// RefreshToken validates a refresh JWT (token_use must be "refresh"), rejects revoked JTIs,
// then rotates: the presented refresh JTI is blacklisted and a new access+refresh pair is issued.
func (s *authService) RefreshToken(ctx context.Context, refreshTokenStr string) (*types.TokenResponse, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "auth.RefreshToken")
	defer span.End()

	token, err := jwt.ParseWithClaims(refreshTokenStr, &jwtClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "parse token failed")
		return nil, fmt.Errorf("%w: %v", ErrInvalidRefreshToken, err)
	}

	claims, ok := token.Claims.(*jwtClaims)
	if !ok || !token.Valid {
		span.SetStatus(otelcodes.Error, "invalid token claims")
		return nil, ErrInvalidRefreshToken
	}

	if claims.Subject == "" {
		span.SetStatus(otelcodes.Error, "missing subject")
		return nil, ErrInvalidRefreshToken
	}

	if claims.TokenUse != "refresh" {
		span.SetStatus(otelcodes.Error, "wrong token type")
		return nil, fmt.Errorf("%w: token is not a refresh token", ErrInvalidRefreshToken)
	}

	if claims.ID != "" {
		exists, err := s.rdb.Exists(ctx, "jwt:blacklist:"+claims.ID).Result()
		if err != nil {
			s.log.Warn("Redis blacklist check failed for refresh token", zap.Error(err))
		} else if exists > 0 {
			span.SetStatus(otelcodes.Error, "token revoked")
			return nil, ErrRefreshTokenRevoked
		}
	}

	user, err := s.repo.GetUserByID(ctx, claims.Subject)
	if errors.Is(err, ErrUserNotFound) {
		span.SetStatus(otelcodes.Error, "user not found")
		return nil, ErrInvalidRefreshToken
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "get user failed")
		return nil, fmt.Errorf("auth: get user: %w", err)
	}

	var rotated bool
	var rotationBlacklistTTL time.Duration
	if claims.ID != "" && claims.ExpiresAt != nil {
		ttl := time.Until(claims.ExpiresAt.Time)
		if ttl > 0 {
			rotationBlacklistTTL = ttl
			span.AddEvent("refresh_token.blacklist_attempt", trace.WithAttributes(
				attribute.Int64("blacklist.ttl_remaining_ms", ttl.Milliseconds()),
			))
			key := "jwt:blacklist:" + claims.ID
			if err := s.rdb.Set(ctx, key, "1", ttl).Err(); err != nil {
				s.log.Error("failed to blacklist refresh token during rotation",
					zap.Error(err),
					zap.String("user_id", user.ID),
				)
				span.RecordError(err)
				span.AddEvent("refresh_token.blacklist_failed", trace.WithAttributes(
					attribute.String("error", err.Error()),
				))
				span.SetStatus(otelcodes.Error, "blacklist old refresh token failed")
				return nil, fmt.Errorf("auth: blacklist refresh token for rotation: %w", err)
			}
			rotated = true
			span.AddEvent("refresh_token.blacklisted", trace.WithAttributes(
				attribute.Int64("blacklist.ttl_remaining_ms", ttl.Milliseconds()),
			))
		}
	}

	span.SetAttributes(attribute.String("user.id", user.ID))
	logFields := []zap.Field{zap.String("user_id", user.ID)}
	if rotated {
		logFields = append(logFields,
			zap.Bool("refresh_token_rotated", true),
			zap.Duration("old_refresh_blacklist_ttl", rotationBlacklistTTL),
		)
	}
	s.log.Info("token refreshed", logFields...)

	return s.issueTokenPair(user)
}

// issueTokenPair signs a new access token (token_use=access) and refresh token (token_use=refresh),
// each with a unique JTI for blacklist-based revocation and rotation.
func (s *authService) issueTokenPair(user *types.User) (*types.TokenResponse, error) {
	now := time.Now()

	accessClaims := jwtClaims{
		UserID:   user.ID,
		Roles:    user.Roles,
		Email:    user.Email,
		TokenUse: "access",
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

	refreshClaims := jwtClaims{
		TokenUse: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(refreshTokenTTL)),
		},
	}

	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("auth: sign refresh token: %w", err)
	}

	return &types.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

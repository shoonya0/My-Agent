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

	"myAgent/pkg/model"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
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
	Register(ctx context.Context, req RegisterRequest) (*model.TokenResponse, error)
	Login(ctx context.Context, req LoginRequest) (*model.TokenResponse, error)
	ValidateToken(ctx context.Context, token string) (*model.Claims, error)
	RevokeToken(ctx context.Context, token string) error
	HandleOAuthCallback(ctx context.Context, req OAuthCallbackParams) (*model.TokenResponse, error)
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
	cfg       *model.Config
	log       *zap.Logger
}

// NewService constructs an auth Service with the required dependencies.
func NewService(repo Repository, rdb *redis.Client, cfg *model.Config, log *zap.Logger) Service {
	return &authService{
		repo:      repo,
		rdb:       rdb,
		jwtSecret: []byte(cfg.JWTSecret),
		cfg:       cfg,
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
		return fmt.Errorf("%w: %v", ErrInvalidLogoutToken, err)
	}

	claims, ok := token.Claims.(*jwtClaims)
	if !ok || claims.ID == "" {
		return ErrLogoutTokenMissingJTI
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
		return fmt.Errorf("auth: blacklist token: %w", err)
	}

	s.log.Info("Token revoked", zap.String("jti", claims.ID))
	return nil
}

func (s *authService) HandleOAuthCallback(ctx context.Context, req OAuthCallbackParams) (*model.TokenResponse, error) {
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

	newUser := &model.User{
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

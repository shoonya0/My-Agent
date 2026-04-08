package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"myAgent/pkg/httputil"
	"myAgent/pkg/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const userContextKey = "user"

// JWTMiddleware returns a Gin middleware that validates JWT tokens from the
// Authorization header, checks the Redis blacklist for revoked tokens, and
// injects an AuthenticatedUser into the request context.
func JWTMiddleware(secret string, rdb *redis.Client) gin.HandlerFunc {
	signingKey := []byte(secret)

	return func(c *gin.Context) {
		token, err := httputil.ExtractBearerToken(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		claims, err := parseToken(token, signingKey)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		if err := checkBlacklist(c.Request.Context(), rdb, claims); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token has been revoked"})
			return
		}

		user := model.AuthenticatedUser{
			UserID: claims.UserID,
			Roles:  claims.Roles,
			Email:  claims.Email,
		}
		c.Set(userContextKey, &user)

		c.Next()
	}
}

// CurrentUser extracts the AuthenticatedUser set by JWTMiddleware.
// Panics if called on a route that is not behind the middleware.
func CurrentUser(c *gin.Context) *model.AuthenticatedUser {
	return c.MustGet(userContextKey).(*model.AuthenticatedUser)
}

// CustomClaims extends jwt.RegisteredClaims with application-specific fields.
type CustomClaims struct {
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
	Email  string   `json:"email"`
	jwt.RegisteredClaims
}

func parseToken(tokenString string, signingKey []byte) (*CustomClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return signingKey, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// checkBlacklist uses Redis to verify the token's JTI has not been revoked.
// A nil redis client skips the check (useful in tests or when Redis is optional).
func checkBlacklist(ctx context.Context, rdb *redis.Client, claims *CustomClaims) error {
	if rdb == nil || claims.ID == "" {
		return nil
	}

	exists, err := rdb.Exists(ctx, "jwt:blacklist:"+claims.ID).Result()
	if err != nil {
		return nil
	}
	if exists > 0 {
		return errors.New("token revoked")
	}

	return nil
}

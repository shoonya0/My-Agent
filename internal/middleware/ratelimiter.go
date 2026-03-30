package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"myAgent/pkg/model"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	rateLimitTTL     = 2 * time.Minute
	headerRateLimit  = "X-RateLimit-Limit"
	headerRemaining  = "X-RateLimit-Remaining"
	headerRetryAfter = "Retry-After"
)

// RateLimiter returns a Gin middleware that enforces a per-minute request cap
// using Redis INCR on key ratelimit:{identity}:{unix_minute}.
// For authenticated routes (placed after JWTMiddleware), the identity is the
// user ID. For unauthenticated routes it falls back to the client IP.
func RateLimiter(rdb *redis.Client, maxPerMinute int) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity := resolveIdentity(c)
		minute := time.Now().UTC().Unix() / 60
		key := fmt.Sprintf("ratelimit:%s:%d", identity, minute)

		count, err := rdb.Incr(c.Request.Context(), key).Result()
		if err != nil {
			c.Next()
			return
		}

		if count == 1 {
			rdb.Expire(c.Request.Context(), key, rateLimitTTL)
		}

		remaining := max(0, int64(maxPerMinute)-count)

		c.Header(headerRateLimit, strconv.Itoa(maxPerMinute))
		c.Header(headerRemaining, strconv.FormatInt(remaining, 10))

		if count > int64(maxPerMinute) {
			retryAfter := 60 - (time.Now().UTC().Unix() % 60)
			c.Header(headerRetryAfter, strconv.FormatInt(retryAfter, 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded, try again later",
			})
			return
		}

		c.Next()
	}
}

func resolveIdentity(c *gin.Context) string {
	if u, exists := c.Get(userContextKey); exists {
		if user, ok := u.(*model.AuthenticatedUser); ok {
			return user.UserID
		}
	}
	return c.ClientIP()
}

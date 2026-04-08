package httputil

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

// ExtractBearerToken extracts and validates a JWT token from the Authorization
// header in the format "Bearer <token>". Returns the token string and an error
// if the header is missing, malformed, or contains an empty token.
func ExtractBearerToken(c *gin.Context) (string, error) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return "", errors.New("authorization header is required")
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("authorization header must be in the format: Bearer <token>")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errors.New("token is empty")
	}

	return token, nil
}

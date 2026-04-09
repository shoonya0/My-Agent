package apigateway

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"myAgent/pkg/middleware/auth"
	"myAgent/pkg/types"
	ws "myAgent/pkg/websocket"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

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
// query parameter. It mirrors the logic in auth.JWTMiddleware but
// supports the query-parameter flow required by browser WebSocket clients.
func (h *GatewayHandler) authenticateWS(c *gin.Context) (*types.AuthenticatedUser, error) {
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

	token, err := jwt.ParseWithClaims(tokenStr, &auth.CustomClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(h.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, errors.New("invalid or expired token")
	}

	claims, ok := token.Claims.(*auth.CustomClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	if claims.ID != "" && h.rdb != nil {
		exists, rErr := h.rdb.Exists(c.Request.Context(), "jwt:blacklist:"+claims.ID).Result()
		if rErr == nil && exists > 0 {
			return nil, errors.New("token has been revoked")
		}
	}

	return &types.AuthenticatedUser{
		UserID: claims.UserID,
		Roles:  claims.Roles,
		Email:  claims.Email,
	}, nil
}

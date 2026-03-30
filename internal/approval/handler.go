package approval

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"myAgent/internal/middleware"
	"myAgent/pkg/model"
	ws "myAgent/pkg/websocket"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Handler exposes HTTP and WebSocket endpoints for the approval service.
type Handler struct {
	svc       *Service
	hub       *ws.Hub
	jwtSecret string
	rdb       *redis.Client
	log       *zap.Logger
}

// NewHandler constructs a Handler with the required dependencies.
func NewHandler(svc *Service, hub *ws.Hub, jwtSecret string, rdb *redis.Client, log *zap.Logger) *Handler {
	return &Handler{
		svc:       svc,
		hub:       hub,
		jwtSecret: jwtSecret,
		rdb:       rdb,
		log:       log,
	}
}

// Routes returns a Gin engine with all approval-service endpoints registered.
func (h *Handler) Routes() http.Handler {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/health", h.Health)
	r.GET("/ws/:job_id", h.HandleWebSocket)

	api := r.Group("/api/v1")
	api.Use(middleware.JWTMiddleware(h.jwtSecret, h.rdb))
	{
		api.POST("/jobs/:job_id/approve", h.HandleApprove)
		api.POST("/jobs/:job_id/reject", h.HandleReject)
	}

	return r
}

// Health is a liveness probe.
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleWebSocket upgrades the connection to a WebSocket for real-time job
// notifications. Authentication is accepted via the Authorization header or
// a ?token= query parameter (for browser clients).
func (h *Handler) HandleWebSocket(c *gin.Context) {
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
		h.log.Error("WebSocket upgrade failed",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
		return
	}

	nodeID := uuid.New().String()
	if regErr := h.svc.RegisterWSSession(c.Request.Context(), jobID, user.UserID, nodeID); regErr != nil {
		h.log.Error("Failed to register WS session",
			zap.Error(regErr),
			zap.String("job_id", jobID),
		)
	}

	client := ws.NewClient(h.hub, conn, jobID, user.UserID)
	h.hub.Register(client)

	go client.WritePump()
	go func() {
		client.ReadPump()
		h.svc.RemoveWSSession(context.Background(), jobID)
	}()
}

// HandleApprove processes POST /api/v1/jobs/:job_id/approve.
func (h *Handler) HandleApprove(c *gin.Context) {
	jobID := c.Param("job_id")
	user := middleware.CurrentUser(c)

	var req model.ApproveJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Platforms) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one platform is required"})
		return
	}

	resp, err := h.svc.Approve(c.Request.Context(), jobID, user.UserID, req)
	if err != nil {
		h.log.Error("Approve failed",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve job"})
		return
	}

	c.JSON(http.StatusAccepted, resp)
}

// HandleReject processes POST /api/v1/jobs/:job_id/reject.
func (h *Handler) HandleReject(c *gin.Context) {
	jobID := c.Param("job_id")
	user := middleware.CurrentUser(c)

	var req model.RejectJobRequest
	// Body is optional — all fields have omitempty.
	_ = c.ShouldBindJSON(&req)

	resp, err := h.svc.Reject(c.Request.Context(), jobID, user.UserID, req)
	if err != nil {
		h.log.Error("Reject failed",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reject job"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// authenticateWS validates JWT from the Authorization header or the ?token=
// query parameter. It mirrors the logic in middleware.JWTMiddleware but
// supports the query-parameter flow required by browser WebSocket clients.
func (h *Handler) authenticateWS(c *gin.Context) (*model.AuthenticatedUser, error) {
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

	token, err := jwt.ParseWithClaims(tokenStr, &middleware.CustomClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(h.jwtSecret), nil
	})
	if err != nil {
		return nil, errors.New("invalid or expired token")
	}

	claims, ok := token.Claims.(*middleware.CustomClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	if claims.ID != "" && h.rdb != nil {
		exists, rErr := h.rdb.Exists(c.Request.Context(), "jwt:blacklist:"+claims.ID).Result()
		if rErr == nil && exists > 0 {
			return nil, errors.New("token has been revoked")
		}
	}

	return &model.AuthenticatedUser{
		UserID: claims.UserID,
		Roles:  claims.Roles,
		Email:  claims.Email,
	}, nil
}

package orchestrator

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Handler holds dependencies for orchestrator HTTP endpoints.
type Handler struct {
	svc Service
	log *zap.Logger
}

// NewHandler constructs an orchestrator Handler.
func NewHandler(svc Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes wires orchestrator endpoints onto the given Gin engine.
// Routes are intended to be called by the api-gateway, which forwards
// the authenticated user's ID via the X-User-ID header.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.health)

	jobs := r.Group("/api/v1/jobs")
	{
		jobs.POST("", h.submitJob)
		jobs.GET("/:job_id", h.getJob)
	}
}

func (h *Handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type submitJobHTTPRequest struct {
	Prompt    string   `json:"prompt" binding:"required,max=1000"`
	ImageURL  string   `json:"image_url" binding:"required"`
	Platforms []string `json:"platforms" binding:"required,min=1"`
	Caption   string   `json:"caption" binding:"max=2200"`
}

func (h *Handler) submitJob(c *gin.Context) {
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing X-User-ID header"})
		return
	}

	var req submitJobHTTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.svc.SubmitJob(c.Request.Context(), userID, SubmitRequest{
		Prompt:    req.Prompt,
		ImageURL:  req.ImageURL,
		Platforms: req.Platforms,
		Caption:   req.Caption,
	})
	if err != nil {
		h.log.Error("Failed to submit job", zap.Error(err), zap.String("user_id", userID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to submit job"})
		return
	}

	c.JSON(http.StatusAccepted, resp)
}

func (h *Handler) getJob(c *gin.Context) {
	jobID := c.Param("job_id")

	resp, err := h.svc.GetJob(c.Request.Context(), jobID)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
			return
		}
		h.log.Error("Failed to get job", zap.Error(err), zap.String("job_id", jobID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve job"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

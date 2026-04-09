package credentials

import (
	"errors"
	"net/http"

	"myAgent/pkg/middleware/auth"
	"myAgent/pkg/messages"
	"myAgent/pkg/types"

	"github.com/gin-gonic/gin"
)

// Handler holds HTTP handlers for platform credential management.
type Handler struct {
	svc Service
}

// NewHandler constructs a credentials Handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes wires credential endpoints onto a router group.
// Expected to be called with the protected /api/v1 group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	creds := rg.Group("/credentials")
	{
		creds.POST("", h.Connect)
		creds.GET("", h.List)
		creds.GET("/:platform", h.Get)
		creds.PUT("/:platform", h.Update)
		creds.DELETE("/:platform", h.Disconnect)
	}
}

// Connect links a social platform by storing an encrypted token.
func (h *Handler) Connect(c *gin.Context) {
	user := auth.CurrentUser(c)

	var req types.ConnectPlatformRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	resp, err := h.svc.Connect(c.Request.Context(), user.UserID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			"Failed to connect platform",
		))
		return
	}

	c.JSON(http.StatusCreated, messages.SuccessResponse(messages.MsgPlatformConnected, resp))
}

// List returns all connected platforms for the authenticated user.
func (h *Handler) List(c *gin.Context) {
	user := auth.CurrentUser(c)

	resp, err := h.svc.List(c.Request.Context(), user.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			"Failed to list platform credentials",
		))
		return
	}

	c.JSON(http.StatusOK, messages.SuccessResponse("Platform credentials retrieved", resp))
}

// Get returns the credential details for a specific platform.
func (h *Handler) Get(c *gin.Context) {
	user := auth.CurrentUser(c)
	platform := c.Param("platform")

	resp, err := h.svc.Get(c.Request.Context(), user.UserID, platform)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			c.JSON(http.StatusNotFound, messages.ErrorResponse(
				messages.ErrCodeNotFound,
				messages.MsgPlatformNotConnected,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			"Failed to get platform credential",
		))
		return
	}

	c.JSON(http.StatusOK, messages.SuccessResponse("Platform credential retrieved", resp))
}

// Update refreshes the token for an already-connected platform.
func (h *Handler) Update(c *gin.Context) {
	user := auth.CurrentUser(c)
	platform := c.Param("platform")

	var req types.UpdatePlatformRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	resp, err := h.svc.Update(c.Request.Context(), user.UserID, platform, req)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			c.JSON(http.StatusNotFound, messages.ErrorResponse(
				messages.ErrCodeNotFound,
				messages.MsgPlatformNotConnected,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			"Failed to update platform credential",
		))
		return
	}

	c.JSON(http.StatusOK, messages.SuccessResponse("Platform credential updated", resp))
}

// Disconnect removes a connected platform.
func (h *Handler) Disconnect(c *gin.Context) {
	user := auth.CurrentUser(c)
	platform := c.Param("platform")

	if err := h.svc.Disconnect(c.Request.Context(), user.UserID, platform); err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			c.JSON(http.StatusNotFound, messages.ErrorResponse(
				messages.ErrCodeNotFound,
				messages.MsgPlatformNotConnected,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			"Failed to disconnect platform",
		))
		return
	}

	c.JSON(http.StatusOK, messages.SuccessResponse(messages.MsgPlatformDisconnected, nil))
}

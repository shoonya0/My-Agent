package apigateway

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"myAgent/api/orchestratorpb"
	"myAgent/pkg/middleware/auth"
	"myAgent/pkg/messages"
	"myAgent/pkg/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SubmitJob accepts an image + prompt and kicks off the editing pipeline.
func (h *GatewayHandler) SubmitJob(c *gin.Context) {
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeMissingField,
			messages.MsgImageRequired,
		))
		return
	}
	defer file.Close()

	if header.Size > maxImageSize {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeFileTooLarge,
			messages.MsgImageTooLarge,
		))
		return
	}

	ct := header.Header.Get("Content-Type")
	if ct != "image/png" && ct != "image/jpeg" && ct != "image/webp" {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeInvalidFileFormat,
			messages.MsgInvalidImageFormat,
		))
		return
	}

	var req types.SubmitJobRequest
	if err := c.ShouldBind(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	if len(req.Platforms) == 0 {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeMissingField,
			messages.MsgPlatformsRequired,
		))
		return
	}

	body, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeInvalidInput,
			"failed to read image upload",
		))
		return
	}

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		switch ct {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/webp":
			ext = ".webp"
		}
	}

	user := auth.CurrentUser(c)
	key := fmt.Sprintf("original/%s/%s%s", user.UserID, uuid.New().String(), ext)
	imageURL, err := h.uploader.Upload(c.Request.Context(), key, body, ct)
	if err != nil {
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			"failed to store image",
		))
		return
	}

	pbResp, err := h.orchClient.SubmitJob(c.Request.Context(), &orchestratorpb.SubmitJobRequest{
		UserId:    user.UserID,
		Prompt:    req.Prompt,
		ImageUrl:  imageURL,
		Platforms: req.Platforms,
		Caption:   req.Caption,
	})
	if err != nil {
		if writeOrchestratorGRPCError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgJobSubmissionFailed,
		))
		return
	}

	resp := types.SubmitJobResponse{
		JobID:     pbResp.GetJobId(),
		Status:    pbResp.GetStatus(),
		WsURL:     pbResp.GetWsUrl(),
		CreatedAt: time.Unix(pbResp.GetCreatedAtUnix(), 0).UTC(),
	}
	c.JSON(http.StatusAccepted, messages.SuccessResponse(messages.MsgJobSubmitted, resp))
}

// GetJob returns the full detail for a single job.
func (h *GatewayHandler) GetJob(c *gin.Context) {
	jobID := c.Param("job_id")
	user := auth.CurrentUser(c)

	pbResp, err := h.orchClient.GetJob(c.Request.Context(), &orchestratorpb.GetJobRequest{
		JobId:  jobID,
		UserId: user.UserID,
	})
	if err != nil {
		if writeOrchestratorGRPCError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgInternalServerError,
		))
		return
	}

	out := types.GetJobResponse{
		ID:                pbResp.GetId(),
		Status:            pbResp.GetStatus(),
		OriginalPrompt:    pbResp.GetOriginalPrompt(),
		RefinedPrompt:     pbResp.GetRefinedPrompt(),
		OriginalImageURL:  pbResp.GetOriginalImageUrl(),
		GeneratedImageURL: pbResp.GetGeneratedImageUrl(),
		CreatedAt:         time.Unix(pbResp.GetCreatedAtUnix(), 0).UTC(),
	}
	for _, pr := range pbResp.GetPostResults() {
		out.PostResults = append(out.PostResults, types.PostResult{
			ID:             pr.GetId(),
			JobID:          pr.GetJobId(),
			UserID:         pr.GetUserId(),
			Platform:       pr.GetPlatform(),
			Status:         pr.GetStatus(),
			PlatformPostID: pr.GetPlatformPostId(),
			PlatformURL:    pr.GetPlatformUrl(),
			ErrorDetail:    pr.GetErrorDetail(),
			AttemptCount:   int(pr.GetAttemptCount()),
			CreatedAt:      time.Unix(pr.GetCreatedAtUnix(), 0).UTC(),
		})
	}

	c.JSON(http.StatusOK, messages.SuccessResponse("Job retrieved", out))
}

// ApproveJob marks a generated image as approved and triggers distribution.
func (h *GatewayHandler) ApproveJob(c *gin.Context) {
	jobID := c.Param("job_id")
	user := auth.CurrentUser(c)

	var req types.ApproveJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp := messages.ParseBindingErrorWithFields(err)
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	if len(req.Platforms) == 0 {
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeMissingField,
			messages.MsgPlatformsRequired,
		))
		return
	}

	pbResp, err := h.orchClient.ApproveJob(c.Request.Context(), &orchestratorpb.ApproveJobRequest{
		JobId:     jobID,
		UserId:    user.UserID,
		Caption:   req.Caption,
		Platforms: req.Platforms,
	})
	if err != nil {
		if writeOrchestratorGRPCError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgJobApprovalFailed,
		))
		return
	}

	resp := types.JobActionResponse{
		JobID:   pbResp.GetJobId(),
		Status:  pbResp.GetStatus(),
		Message: pbResp.GetMessage(),
	}
	c.JSON(http.StatusAccepted, messages.SuccessResponse(messages.MsgJobApproved, resp))
}

// RejectJob marks a generated image as rejected.
func (h *GatewayHandler) RejectJob(c *gin.Context) {
	jobID := c.Param("job_id")
	user := auth.CurrentUser(c)

	var req types.RejectJobRequest
	_ = c.ShouldBindJSON(&req)

	pbResp, err := h.orchClient.RejectJob(c.Request.Context(), &orchestratorpb.RejectJobRequest{
		JobId:  jobID,
		UserId: user.UserID,
		Reason: req.Reason,
	})
	if err != nil {
		if writeOrchestratorGRPCError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, messages.ErrorResponse(
			messages.ErrCodeInternalServer,
			messages.MsgJobRejectionFailed,
		))
		return
	}

	resp := types.JobActionResponse{
		JobID:   pbResp.GetJobId(),
		Status:  pbResp.GetStatus(),
		Message: pbResp.GetMessage(),
	}
	c.JSON(http.StatusOK, messages.SuccessResponse(messages.MsgJobRejected, resp))
}

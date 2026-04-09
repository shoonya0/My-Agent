package apigateway

import (
	"net/http"

	"myAgent/pkg/messages"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// writeOrchestratorGRPCError maps orchestrator gRPC status codes to HTTP responses.
// It returns true when err was handled.
func writeOrchestratorGRPCError(c *gin.Context, err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.NotFound:
		c.JSON(http.StatusNotFound, messages.ErrorResponse(
			messages.ErrCodeJobNotFound,
			messages.MsgJobNotFound,
		))
	case codes.PermissionDenied:
		c.JSON(http.StatusForbidden, messages.ErrorResponse(
			messages.ErrCodeUnauthorized,
			messages.MsgUnauthorized,
		))
	case codes.InvalidArgument:
		c.JSON(http.StatusBadRequest, messages.ErrorResponse(
			messages.ErrCodeInvalidInput,
			st.Message(),
		))
	case codes.FailedPrecondition:
		c.JSON(http.StatusConflict, messages.ErrorResponse(
			messages.ErrCodeOperationFailed,
			st.Message(),
		))
	default:
		return false
	}
	return true
}

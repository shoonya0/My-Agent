package orchestrator

import (
	"context"
	"errors"

	"myAgent/api/orchestratorpb"
	"myAgent/pkg/grpcserver"
	"myAgent/pkg/model"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Handler implements orchestrator.v1.OrchestratorService.
type Handler struct {
	orchestratorpb.UnimplementedOrchestratorServiceServer
	svc Service
	log *zap.Logger
}

// NewHandler constructs an orchestrator gRPC Handler.
func NewHandler(svc Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// GRPCRegistrar returns a registrar for the orchestrator gRPC service.
func (h *Handler) GRPCRegistrar() grpcserver.Registrar {
	return func(srv *grpc.Server) {
		orchestratorpb.RegisterOrchestratorServiceServer(srv, h)
	}
}

// SubmitJob implements orchestratorpb.OrchestratorServiceServer.
func (h *Handler) SubmitJob(ctx context.Context, req *orchestratorpb.SubmitJobRequest) (*orchestratorpb.SubmitJobResponse, error) {
	if req.GetUserId() == "" || req.GetPrompt() == "" || req.GetImageUrl() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id, prompt, and image_url are required")
	}
	if len(req.GetPlatforms()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one platform is required")
	}

	resp, err := h.svc.SubmitJob(ctx, req.GetUserId(), SubmitRequest{
		Prompt:    req.GetPrompt(),
		ImageURL:  req.GetImageUrl(),
		Platforms: req.GetPlatforms(),
		Caption:   req.GetCaption(),
	})
	if err != nil {
		h.log.Error("SubmitJob failed", zap.Error(err), zap.String("user_id", req.GetUserId()))
		return nil, status.Errorf(codes.Internal, "failed to submit job")
	}

	return &orchestratorpb.SubmitJobResponse{
		JobId:           resp.JobID,
		Status:          resp.Status,
		WsUrl:           resp.WsURL,
		CreatedAtUnix:   resp.CreatedAt.Unix(),
	}, nil
}

// GetJob implements orchestratorpb.OrchestratorServiceServer.
func (h *Handler) GetJob(ctx context.Context, req *orchestratorpb.GetJobRequest) (*orchestratorpb.GetJobResponse, error) {
	if req.GetJobId() == "" || req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id and user_id are required")
	}

	resp, err := h.svc.GetJob(ctx, req.GetJobId(), req.GetUserId())
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			return nil, status.Error(codes.NotFound, "job not found")
		}
		if errors.Is(err, ErrJobAccessDenied) {
			return nil, status.Error(codes.PermissionDenied, "access denied")
		}
		h.log.Error("GetJob failed", zap.Error(err), zap.String("job_id", req.GetJobId()))
		return nil, status.Errorf(codes.Internal, "failed to get job")
	}

	out := &orchestratorpb.GetJobResponse{
		Id:                resp.ID,
		Status:            resp.Status,
		OriginalPrompt:    resp.OriginalPrompt,
		RefinedPrompt:     resp.RefinedPrompt,
		OriginalImageUrl:  resp.OriginalImageURL,
		GeneratedImageUrl: resp.GeneratedImageURL,
		CreatedAtUnix:     resp.CreatedAt.Unix(),
	}
	for _, pr := range resp.PostResults {
		out.PostResults = append(out.PostResults, &orchestratorpb.PostResultMsg{
			Id:              pr.ID,
			JobId:           pr.JobID,
			UserId:          pr.UserID,
			Platform:        pr.Platform,
			Status:          pr.Status,
			PlatformPostId:  pr.PlatformPostID,
			PlatformUrl:     pr.PlatformURL,
			ErrorDetail:     pr.ErrorDetail,
			AttemptCount:    int32(pr.AttemptCount),
			CreatedAtUnix:   pr.CreatedAt.Unix(),
		})
	}
	return out, nil
}

// ApproveJob implements orchestratorpb.OrchestratorServiceServer.
func (h *Handler) ApproveJob(ctx context.Context, req *orchestratorpb.ApproveJobRequest) (*orchestratorpb.JobActionResponse, error) {
	if req.GetJobId() == "" || req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id and user_id are required")
	}
	if len(req.GetPlatforms()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one platform is required")
	}

	resp, err := h.svc.ApproveJob(ctx, req.GetJobId(), req.GetUserId(), model.ApproveJobRequest{
		Caption:   req.GetCaption(),
		Platforms: req.GetPlatforms(),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrJobNotFound):
			return nil, status.Error(codes.NotFound, "job not found")
		case errors.Is(err, ErrJobAccessDenied):
			return nil, status.Error(codes.PermissionDenied, "access denied")
		case errors.Is(err, ErrPreviewNotReady):
			return nil, status.Error(codes.FailedPrecondition, "image preview not ready")
		case errors.Is(err, ErrInvalidJobState):
			return nil, status.Error(codes.FailedPrecondition, "job is not awaiting approval")
		default:
			h.log.Error("ApproveJob failed", zap.Error(err), zap.String("job_id", req.GetJobId()))
			return nil, status.Errorf(codes.Internal, "failed to approve job")
		}
	}

	return &orchestratorpb.JobActionResponse{
		JobId:   resp.JobID,
		Status:  resp.Status,
		Message: resp.Message,
	}, nil
}

// RejectJob implements orchestratorpb.OrchestratorServiceServer.
func (h *Handler) RejectJob(ctx context.Context, req *orchestratorpb.RejectJobRequest) (*orchestratorpb.JobActionResponse, error) {
	if req.GetJobId() == "" || req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id and user_id are required")
	}

	resp, err := h.svc.RejectJob(ctx, req.GetJobId(), req.GetUserId(), model.RejectJobRequest{
		Reason: req.GetReason(),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrJobNotFound):
			return nil, status.Error(codes.NotFound, "job not found")
		case errors.Is(err, ErrJobAccessDenied):
			return nil, status.Error(codes.PermissionDenied, "access denied")
		case errors.Is(err, ErrInvalidJobState):
			return nil, status.Error(codes.FailedPrecondition, "job is not awaiting approval")
		default:
			h.log.Error("RejectJob failed", zap.Error(err), zap.String("job_id", req.GetJobId()))
			return nil, status.Errorf(codes.Internal, "failed to reject job")
		}
	}

	return &orchestratorpb.JobActionResponse{
		JobId:   resp.JobID,
		Status:  resp.Status,
		Message: resp.Message,
	}, nil
}

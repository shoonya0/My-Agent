package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"myAgent/pkg/kafka"
	"myAgent/pkg/llm"
	"myAgent/pkg/model"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

const (
	tracerName                 = "internal/orchestrator"
	topicPromptRefineRequested = "prompt.refine.requested"
	topicJobFailed             = "job.failed"
	topicImageApproved         = "image.approved"
	serviceName                = "orchestrator"
	previewKeyPrefix           = "job:preview:"
)

// Service defines the business logic for job orchestration.
type Service interface {
	SubmitJob(ctx context.Context, userID string, req SubmitRequest) (*model.SubmitJobResponse, error)
	GetJob(ctx context.Context, jobID, userID string) (*model.GetJobResponse, error)
	ApproveJob(ctx context.Context, jobID, userID string, req model.ApproveJobRequest) (*model.JobActionResponse, error)
	RejectJob(ctx context.Context, jobID, userID string, req model.RejectJobRequest) (*model.JobActionResponse, error)
}

// SubmitRequest is the service-layer input for job submission.
type SubmitRequest struct {
	Prompt    string
	ImageURL  string
	Platforms []string
	Caption   string
}

type orchestratorService struct {
	repo     Repository
	producer kafka.Producer
	llm      llm.Client
	rdb      *redis.Client
	log      *zap.Logger
}

// NewService constructs an orchestrator Service with the required dependencies.
func NewService(repo Repository, producer kafka.Producer, llmClient llm.Client, rdb *redis.Client, log *zap.Logger) Service {
	return &orchestratorService{
		repo:     repo,
		producer: producer,
		llm:      llmClient,
		rdb:      rdb,
		log:      log,
	}
}

// SubmitJob creates a job record, calls the LLM to parse the user's intent
// into an ExecutionPlan, then publishes a PromptRefinementJob event to Kafka.
func (s *orchestratorService) SubmitJob(ctx context.Context, userID string, req SubmitRequest) (*model.SubmitJobResponse, error) {
	// It starts a new span for the SubmitJob operation means it will be used to track the SubmitJob operation
	ctx, span := otel.Tracer(tracerName).Start(ctx, "orchestrator.SubmitJob")
	defer span.End()

	jobID := uuid.NewString()
	span.SetAttributes(
		attribute.String("job.id", jobID),
		attribute.String("user.id", userID),
	)

	job := &model.Job{
		ID:               jobID,
		UserID:           userID,
		Status:           model.JobStatusPending,
		OriginalPrompt:   req.Prompt,
		OriginalImageURL: req.ImageURL,
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("orchestrator: create job: %w", err)
	}
	s.recordTransition(ctx, jobID, "", model.JobStatusPending)

	plan, err := s.llm.ParseIntent(ctx, req.Prompt)
	if err != nil {
		s.failJob(ctx, jobID, userID, fmt.Sprintf("parse intent: %v", err))
		return nil, fmt.Errorf("orchestrator: parse intent: %w", err)
	}

	planJSON, err := json.Marshal(plan)
	if err != nil {
		s.failJob(ctx, jobID, userID, fmt.Sprintf("marshal plan: %v", err))
		return nil, fmt.Errorf("orchestrator: marshal execution plan: %w", err)
	}

	job.ExecutionPlan = planJSON
	job.Status = model.JobStatusRefining
	if err := s.repo.UpdateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("orchestrator: update job: %w", err)
	}
	s.recordTransition(ctx, jobID, model.JobStatusPending, model.JobStatusRefining)

	event := model.PromptRefinementJob{
		JobID:          jobID,
		UserID:         userID,
		OriginalPrompt: req.Prompt,
		ImageURL:       req.ImageURL,
		ExecutionPlan:  *plan,
		TraceCtx:       extractTraceCtx(ctx),
		PublishedAt:    time.Now(),
	}
	if err := s.producer.Publish(ctx, topicPromptRefineRequested, jobID, event); err != nil {
		s.failJob(ctx, jobID, userID, fmt.Sprintf("publish refinement event: %v", err))
		return nil, fmt.Errorf("orchestrator: publish event: %w", err)
	}

	s.log.Info("Job submitted and refinement requested",
		zap.String("job_id", jobID),
		zap.String("user_id", userID),
	)

	return &model.SubmitJobResponse{
		JobID:     jobID,
		Status:    model.JobStatusRefining,
		WsURL:     fmt.Sprintf("/ws/%s", jobID),
		CreatedAt: job.CreatedAt,
	}, nil
}

// GetJob fetches a job by ID and maps it to the API response contract.
func (s *orchestratorService) GetJob(ctx context.Context, jobID, userID string) (*model.GetJobResponse, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "orchestrator.GetJob")
	defer span.End()

	span.SetAttributes(attribute.String("job.id", jobID))

	job, err := s.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: get job: %w", err)
	}
	if job.UserID != userID {
		return nil, ErrJobAccessDenied
	}

	postResults, err := s.repo.ListPostResultsByJobID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: list post results: %w", err)
	}

	return &model.GetJobResponse{
		ID:                job.ID,
		Status:            job.Status,
		OriginalPrompt:    job.OriginalPrompt,
		RefinedPrompt:     job.RefinedPrompt,
		OriginalImageURL:  job.OriginalImageURL,
		GeneratedImageURL: job.GeneratedImageURL,
		PostResults:       postResults,
		CreatedAt:         job.CreatedAt,
	}, nil
}

// ApproveJob publishes image.approved after validating ownership and preview availability.
func (s *orchestratorService) ApproveJob(ctx context.Context, jobID, userID string, req model.ApproveJobRequest) (*model.JobActionResponse, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "orchestrator.ApproveJob")
	defer span.End()
	span.SetAttributes(attribute.String("job.id", jobID), attribute.String("user.id", userID))

	job, err := s.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: get job: %w", err)
	}
	if job.UserID != userID {
		return nil, ErrJobAccessDenied
	}
	if !s.jobAllowsUserDecision(ctx, jobID, job.Status) {
		return nil, ErrInvalidJobState
	}

	imageURL, err := s.resolveGeneratedImageURL(ctx, jobID, job.GeneratedImageURL)
	if err != nil {
		return nil, err
	}

	fromStatus := job.Status
	event := model.ImageApprovedEvent{
		JobID:     jobID,
		UserID:    userID,
		ImageURL:  imageURL,
		Caption:   req.Caption,
		Platforms: req.Platforms,
		TraceCtx:  extractTraceCtx(ctx),
	}
	if err := s.producer.Publish(ctx, topicImageApproved, jobID, event); err != nil {
		return nil, fmt.Errorf("orchestrator: publish image.approved: %w", err)
	}

	if err := s.repo.UpdateJobStatus(ctx, jobID, model.JobStatusDistributing); err != nil {
		return nil, fmt.Errorf("orchestrator: update job status: %w", err)
	}
	s.recordTransition(ctx, jobID, fromStatus, model.JobStatusDistributing)

	s.log.Info("Job approved via orchestrator",
		zap.String("job_id", jobID),
		zap.String("user_id", userID),
		zap.Strings("platforms", req.Platforms),
	)

	return &model.JobActionResponse{
		JobID:   jobID,
		Status:  model.JobStatusDistributing,
		Message: "Image approved and distribution started.",
	}, nil
}

// RejectJob marks the job rejected and publishes job.failed (same semantics as approval-service reject).
func (s *orchestratorService) RejectJob(ctx context.Context, jobID, userID string, req model.RejectJobRequest) (*model.JobActionResponse, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "orchestrator.RejectJob")
	defer span.End()
	span.SetAttributes(attribute.String("job.id", jobID), attribute.String("user.id", userID))

	job, err := s.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: get job: %w", err)
	}
	if job.UserID != userID {
		return nil, ErrJobAccessDenied
	}
	if !s.jobAllowsUserDecision(ctx, jobID, job.Status) {
		return nil, ErrInvalidJobState
	}

	reason := req.Reason
	if reason == "" {
		reason = "rejected by user"
	}

	fromStatus := job.Status
	if err := s.repo.UpdateJobStatus(ctx, jobID, model.JobStatusRejected); err != nil {
		return nil, fmt.Errorf("orchestrator: update job status: %w", err)
	}
	s.recordTransition(ctx, jobID, fromStatus, model.JobStatusRejected)

	evt := model.JobFailedEvent{
		JobID:        jobID,
		UserID:       userID,
		FailedAt:     serviceName,
		ErrorMessage: reason,
		TraceCtx:     extractTraceCtx(ctx),
	}
	if err := s.producer.Publish(ctx, topicJobFailed, jobID, evt); err != nil {
		s.log.Error("Failed to publish job.failed on reject",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
	}

	if s.rdb != nil {
		if err := s.rdb.Del(ctx, previewKeyPrefix+jobID).Err(); err != nil {
			s.log.Warn("Failed to delete preview cache",
				zap.Error(err),
				zap.String("job_id", jobID),
			)
		}
	}

	s.log.Info("Job rejected via orchestrator",
		zap.String("job_id", jobID),
		zap.String("user_id", userID),
	)

	return &model.JobActionResponse{
		JobID:   jobID,
		Status:  model.JobStatusRejected,
		Message: "Image rejected.",
	}, nil
}

// jobAllowsUserDecision is true when the job is in awaiting_approval or a preview
// exists in Redis (approval-service may cache previews before the jobs row is updated).
func (s *orchestratorService) jobAllowsUserDecision(ctx context.Context, jobID, jobStatus string) bool {
	if jobStatus == model.JobStatusAwaitingApproval {
		return true
	}
	if s.rdb == nil {
		return false
	}
	n, err := s.rdb.Exists(ctx, previewKeyPrefix+jobID).Result()
	return err == nil && n > 0
}

func (s *orchestratorService) resolveGeneratedImageURL(ctx context.Context, jobID, generatedURL string) (string, error) {
	if s.rdb != nil {
		data, err := s.rdb.Get(ctx, previewKeyPrefix+jobID).Bytes()
		if err == nil {
			var preview model.JobPreviewCache
			if err := json.Unmarshal(data, &preview); err == nil && preview.ImageURL != "" {
				return preview.ImageURL, nil
			}
		} else if !errors.Is(err, redis.Nil) {
			return "", fmt.Errorf("orchestrator: redis get preview: %w", err)
		}
	}
	if generatedURL != "" {
		return generatedURL, nil
	}
	return "", ErrPreviewNotReady
}

// failJob marks the job as failed in the database and publishes a job.failed
// event. Errors are logged but not propagated to avoid masking the root cause.
func (s *orchestratorService) failJob(ctx context.Context, jobID, userID, errMsg string) {
	if err := s.repo.UpdateJobStatus(ctx, jobID, model.JobStatusFailed); err != nil {
		s.log.Error("Failed to mark job as failed",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
	}
	s.recordTransition(ctx, jobID, "", model.JobStatusFailed)

	evt := model.JobFailedEvent{
		JobID:        jobID,
		UserID:       userID,
		FailedAt:     serviceName,
		ErrorMessage: errMsg,
		TraceCtx:     extractTraceCtx(ctx),
	}
	if err := s.producer.Publish(ctx, topicJobFailed, jobID, evt); err != nil {
		s.log.Error("Failed to publish job.failed event",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
	}
}

func (s *orchestratorService) recordTransition(ctx context.Context, jobID, from, to string) {
	h := &model.JobStatusHistory{
		ID:         uuid.NewString(),
		JobID:      jobID,
		FromStatus: from,
		ToStatus:   to,
		Service:    serviceName,
	}
	if err := s.repo.InsertStatusHistory(ctx, h); err != nil {
		s.log.Error("Failed to record status transition",
			zap.Error(err),
			zap.String("job_id", jobID),
			zap.String("from", from),
			zap.String("to", to),
		)
	}
}

// extractTraceCtx returns the W3C trace context map for Kafka event propagation.
func extractTraceCtx(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

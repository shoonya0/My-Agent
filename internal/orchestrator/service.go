package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"myAgent/pkg/kafka"
	"myAgent/pkg/llm"
	"myAgent/pkg/model"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

const (
	tracerName                 = "internal/orchestrator"
	topicPromptRefineRequested = "prompt.refine.requested"
	topicJobFailed             = "job.failed"
	serviceName                = "orchestrator"
)

// Service defines the business logic for job orchestration.
type Service interface {
	SubmitJob(ctx context.Context, userID string, req SubmitRequest) (*model.SubmitJobResponse, error)
	GetJob(ctx context.Context, jobID string) (*model.GetJobResponse, error)
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
	log      *zap.Logger
}

// NewService constructs an orchestrator Service with the required dependencies.
func NewService(repo Repository, producer kafka.Producer, llmClient llm.Client, log *zap.Logger) Service {
	return &orchestratorService{
		repo:     repo,
		producer: producer,
		llm:      llmClient,
		log:      log,
	}
}

// SubmitJob creates a job record, calls the LLM to parse the user's intent
// into an ExecutionPlan, then publishes a PromptRefinementJob event to Kafka.
func (s *orchestratorService) SubmitJob(ctx context.Context, userID string, req SubmitRequest) (*model.SubmitJobResponse, error) {
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
func (s *orchestratorService) GetJob(ctx context.Context, jobID string) (*model.GetJobResponse, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "orchestrator.GetJob")
	defer span.End()

	span.SetAttributes(attribute.String("job.id", jobID))

	job, err := s.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: get job: %w", err)
	}

	return &model.GetJobResponse{
		ID:                job.ID,
		Status:            job.Status,
		OriginalPrompt:    job.OriginalPrompt,
		RefinedPrompt:     job.RefinedPrompt,
		OriginalImageURL:  job.OriginalImageURL,
		GeneratedImageURL: job.GeneratedImageURL,
		CreatedAt:         job.CreatedAt,
	}, nil
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

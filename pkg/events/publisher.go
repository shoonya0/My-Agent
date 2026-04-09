package events

import (
	"context"
	"encoding/json"
	"time"

	"myAgent/pkg/data/kafka"
	"myAgent/pkg/types"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

const (
	TopicJobFailed = "job.failed"
)

// PublishJobFailed publishes a job failure event to Kafka with OpenTelemetry trace propagation.
// This consolidates duplicate publishFailure logic from prompt-agent, image-gen-agent, and distribution-service.
func PublishJobFailed(
	ctx context.Context,
	producer kafka.Producer,
	jobID, userID, serviceName, errorMsg string,
	log *zap.Logger,
) error {
	traceCtx := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(traceCtx))

	event := types.JobFailedEvent{
		JobID:        jobID,
		UserID:       userID,
		FailedAt:     serviceName,
		ErrorMessage: errorMsg,
		TraceCtx:     traceCtx,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Error("Failed to marshal job.failed event",
			zap.String("job_id", jobID),
			zap.Error(err),
		)
		return err
	}

	if err := producer.Publish(ctx, TopicJobFailed, jobID, payload); err != nil {
		log.Error("Failed to publish job.failed event",
			zap.String("job_id", jobID),
			zap.Error(err),
		)
		return err
	}

	log.Info("Published job.failed event",
		zap.String("job_id", jobID),
		zap.String("failed_at", serviceName),
	)
	return nil
}

// NewJobFailedEvent creates a JobFailedEvent with the given parameters.
// Useful for tests or when you need to construct the event without immediately publishing.
func NewJobFailedEvent(jobID, userID, failedAt, errorMsg string, traceCtx map[string]string) *types.JobFailedEvent {
	return &types.JobFailedEvent{
		JobID:        jobID,
		UserID:       userID,
		FailedAt:     failedAt,
		ErrorMessage: errorMsg,
		TraceCtx:     traceCtx,
	}
}

// NewImageApprovedEvent creates an ImageApprovedEvent with the given parameters and trace context from ctx.
func NewImageApprovedEvent(ctx context.Context, jobID, userID, imageURL, caption string, platforms []string) *types.ImageApprovedEvent {
	traceCtx := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(traceCtx))

	return &types.ImageApprovedEvent{
		JobID:     jobID,
		UserID:    userID,
		ImageURL:  imageURL,
		Caption:   caption,
		Platforms: platforms,
		TraceCtx:  traceCtx,
	}
}

// NewPromptRefinementJob creates a PromptRefinementJob with the given parameters and trace context from ctx.
func NewPromptRefinementJob(
	ctx context.Context,
	jobID, userID, originalPrompt, imageURL string,
	executionPlan types.ExecutionPlan,
) *types.PromptRefinementJob {
	traceCtx := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(traceCtx))

	return &types.PromptRefinementJob{
		JobID:          jobID,
		UserID:         userID,
		OriginalPrompt: originalPrompt,
		ImageURL:       imageURL,
		ExecutionPlan:  executionPlan,
		TraceCtx:       traceCtx,
		PublishedAt:    time.Now().UTC(),
	}
}

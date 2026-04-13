package promptagent

import (
	"context"
	"encoding/json"
	"fmt"

	"myAgent/pkg/events"
	"myAgent/pkg/data/kafka"
	"myAgent/pkg/llm"
	"myAgent/pkg/types"
	apmotel "myAgent/pkg/infrastructure/otel"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const (
	tracerName   = "internal/workers/promptagent"
	topicRefined = "prompt.refined"
	serviceName  = "prompt-agent"
)

// Worker consumes prompt.refine.requested events, calls the LLM to rewrite
// the prompt, and publishes prompt.refined events. On failure it publishes
// a job.failed event so the orchestrator can mark the job accordingly.
type Worker struct {
	consumer kafka.Consumer
	producer kafka.Producer
	refiner  llm.PromptRefiner
	repo     Repository
	log      *zap.Logger
}

// NewWorker constructs a Worker with the required dependencies.
func NewWorker(consumer kafka.Consumer, producer kafka.Producer, refiner llm.PromptRefiner, repo Repository, log *zap.Logger) *Worker {
	return &Worker{
		consumer: consumer,
		producer: producer,
		refiner:  refiner,
		repo:     repo,
		log:      log,
	}
}

// Run starts the Kafka consume loop. It blocks until ctx is cancelled,
// processing each PromptRefinementJob through the LLM refiner.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("Prompt-agent worker starting")
	return w.consumer.Consume(ctx, w.handle)
}

func (w *Worker) handle(ctx context.Context, msg *kafka.Message) error {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "promptagent.HandleMessage")
	defer span.End()

	var job types.PromptRefinementJob
	if err := json.Unmarshal(msg.Value, &job); err != nil {
		w.log.Error("Failed to unmarshal refinement job",
			zap.Error(err),
			zap.String("topic", msg.Topic),
			zap.Int64("offset", msg.Offset),
		)
		return fmt.Errorf("unmarshal refinement job: %w", err)
	}

	span.SetAttributes(
		attribute.String("job.id", job.JobID),
		attribute.String("user.id", job.UserID),
	)

	w.log.Info("Processing prompt refinement",
		zap.String("job_id", job.JobID),
		zap.String("user_id", job.UserID),
	)

	refined, err := w.refiner.RefinePrompt(ctx, job.OriginalPrompt, job.ExecutionPlan)
	if err != nil {
		w.log.Error("LLM refinement failed",
			zap.Error(err),
			zap.String("job_id", job.JobID),
		)
		w.publishFailure(ctx, job.JobID, job.UserID, fmt.Sprintf("refine prompt: %v", err))
		return fmt.Errorf("refine prompt for job %s: %w", job.JobID, err)
	}

	if err := w.repo.UpdateRefinedPrompt(ctx, job.JobID, refined.Prompt); err != nil {
		w.log.Error("Failed to persist refined prompt",
			zap.Error(err),
			zap.String("job_id", job.JobID),
		)
		w.publishFailure(ctx, job.JobID, job.UserID, fmt.Sprintf("persist refined prompt: %v", err))
		return fmt.Errorf("update refined_prompt for job %s: %w", job.JobID, err)
	}

	event := types.RefinedPromptEvent{
		JobID:            job.JobID,
		UserID:           job.UserID,
		RefinedPrompt:    refined.Prompt,
		StyleParams:      refined.StyleParams,
		OriginalImageURL: job.ImageURL,
		TraceCtx:         apmotel.ExtractTraceContext(ctx),
	}

	if err := w.producer.Publish(ctx, topicRefined, job.JobID, event); err != nil {
		w.log.Error("Failed to publish refined event",
			zap.Error(err),
			zap.String("job_id", job.JobID),
		)
		w.publishFailure(ctx, job.JobID, job.UserID, fmt.Sprintf("publish refined event: %v", err))
		return fmt.Errorf("publish refined event for job %s: %w", job.JobID, err)
	}

	w.log.Info("Prompt refined and published",
		zap.String("job_id", job.JobID),
		zap.Int("refined_len", len(refined.Prompt)),
	)

	return nil
}

// publishFailure sends a job.failed event so the orchestrator can mark the
// job as failed. Errors are logged but not returned to avoid masking the
// root cause.
func (w *Worker) publishFailure(ctx context.Context, jobID, userID, errMsg string) {
	_ = events.PublishJobFailed(ctx, w.producer, jobID, userID, serviceName, errMsg, w.log)
}


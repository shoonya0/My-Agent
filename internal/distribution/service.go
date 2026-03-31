package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"myAgent/pkg/connectors"
	"myAgent/pkg/kafka"
	"myAgent/pkg/model"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	topicJobFailed = "job.failed"
	serviceName    = "distribution"
)

// Service consumes ImageApprovedEvents and fans out posting jobs to
// registered platform connectors in parallel.
type Service struct {
	consumer kafka.Consumer
	producer kafka.Producer
	registry *connectors.Registry
	repo     *Repository
	log      *zap.Logger
}

// NewService creates a distribution Service with all required dependencies.
func NewService(
	consumer kafka.Consumer,
	producer kafka.Producer,
	registry *connectors.Registry,
	repo *Repository,
	log *zap.Logger,
) *Service {
	return &Service{
		consumer: consumer,
		producer: producer,
		registry: registry,
		repo:     repo,
		log:      log,
	}
}

// Run blocks and consumes image.approved events until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	return s.consumer.Consume(ctx, s.handleMessage)
}

func (s *Service) handleMessage(ctx context.Context, msg *kafka.Message) error {
	var evt model.ImageApprovedEvent
	if err := json.Unmarshal(msg.Value, &evt); err != nil {
		s.log.Error("Failed to unmarshal ImageApprovedEvent",
			zap.Error(err),
			zap.String("topic", msg.Topic),
			zap.Int64("offset", msg.Offset),
		)
		return fmt.Errorf("unmarshal event: %w", err)
	}

	s.log.Info("Received image.approved event",
		zap.String("job_id", evt.JobID),
		zap.String("user_id", evt.UserID),
		zap.Strings("platforms", evt.Platforms),
	)

	return s.distribute(ctx, evt)
}

// distribute fans out the approved image to all requested platforms
// concurrently. Individual connector failures are recorded but do not
// block other platforms. Only when every platform fails is a job.failed
// event published.
func (s *Service) distribute(ctx context.Context, evt model.ImageApprovedEvent) error {
	ctx, span := otel.Tracer(serviceName).Start(ctx, "distribute")
	defer span.End()
	span.SetAttributes(
		attribute.String("job.id", evt.JobID),
		attribute.String("user.id", evt.UserID),
		attribute.Int("platforms.count", len(evt.Platforms)),
	)

	postReq := model.PostRequest{
		MediaURL: evt.ImageURL,
		Caption:  evt.Caption,
	}

	var (
		mu      sync.Mutex
		results []model.PostResult
	)

	var g errgroup.Group
	for _, platform := range evt.Platforms {
		g.Go(func() error {
			pr := s.publishToPlatform(ctx, evt, postReq, platform)
			mu.Lock()
			results = append(results, pr)
			mu.Unlock()
			return nil // never cancel siblings
		})
	}
	_ = g.Wait()

	for i := range results {
		if err := s.repo.InsertResult(ctx, &results[i]); err != nil {
			s.log.Error("Failed to persist post result",
				zap.String("job_id", evt.JobID),
				zap.String("platform", results[i].Platform),
				zap.Error(err),
			)
		}
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "success" {
			successCount++
		}
	}

	if successCount == 0 && len(results) > 0 {
		s.publishJobFailed(ctx, evt, "all platform connectors failed")
		span.SetStatus(codes.Error, "all platforms failed")
	} else {
		span.SetStatus(codes.Ok, fmt.Sprintf("%d/%d succeeded", successCount, len(results)))
	}

	s.log.Info("Distribution complete",
		zap.String("job_id", evt.JobID),
		zap.Int("success", successCount),
		zap.Int("total", len(results)),
	)
	return nil
}

func (s *Service) publishToPlatform(
	ctx context.Context,
	evt model.ImageApprovedEvent,
	req model.PostRequest,
	platform string,
) model.PostResult {
	pr := model.PostResult{
		ID:           uuid.New().String(),
		JobID:        evt.JobID,
		UserID:       evt.UserID,
		Platform:     platform,
		AttemptCount: 1,
		CreatedAt:    time.Now(),
	}

	connector, err := s.registry.Get(platform)
	if err != nil {
		s.log.Warn("No connector for platform",
			zap.String("platform", platform),
			zap.String("job_id", evt.JobID),
		)
		pr.Status = "failed"
		pr.ErrorDetail = err.Error()
		return pr
	}

	result, err := connector.Publish(ctx, req)
	if err != nil {
		s.log.Error("Connector publish failed",
			zap.String("platform", platform),
			zap.String("job_id", evt.JobID),
			zap.Error(err),
		)
		pr.Status = "failed"
		pr.ErrorDetail = err.Error()
		return pr
	}

	pr.Status = "success"
	if result != nil {
		pr.PlatformPostID = result.PlatformPostID
		pr.PlatformURL = result.PlatformURL
	}

	s.log.Info("Published to platform",
		zap.String("platform", platform),
		zap.String("job_id", evt.JobID),
		zap.String("platform_post_id", pr.PlatformPostID),
	)
	return pr
}

func (s *Service) publishJobFailed(ctx context.Context, evt model.ImageApprovedEvent, reason string) {
	failEvt := model.JobFailedEvent{
		JobID:        evt.JobID,
		UserID:       evt.UserID,
		FailedAt:     serviceName,
		ErrorMessage: reason,
		TraceCtx:     evt.TraceCtx,
	}

	if err := s.producer.Publish(ctx, topicJobFailed, evt.JobID, failEvt); err != nil {
		s.log.Error("Failed to publish job.failed event",
			zap.String("job_id", evt.JobID),
			zap.Error(err),
		)
	}
}

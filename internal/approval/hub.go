package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"myAgent/api/approvalpb"
	"myAgent/pkg/kafka"
	"myAgent/pkg/model"
	"myAgent/pkg/storage"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const (
	tracerName = "internal/approval"

	previewKeyPrefix = "job:preview:"
	previewTTL       = 1 * time.Hour
)

// Service bridges Kafka image.generated events to gRPC stream notifications
// (approve/reject flows through api-gateway → orchestrator).
type Service struct {
	consumer      kafka.Consumer
	streamManager *StreamManager
	rdb           *redis.Client
	presigner     storage.Presigner
	bucket        string
	endpoint      string
	log           *zap.Logger
}

// NewService constructs a Service with the required dependencies. presigner
// may be nil — in that case the raw object URL is used for previews.
func NewService(
	consumer kafka.Consumer,
	streamManager *StreamManager,
	rdb *redis.Client,
	presigner storage.Presigner,
	bucket, endpoint string,
	log *zap.Logger,
) *Service {
	return &Service{
		consumer:      consumer,
		streamManager: streamManager,
		rdb:           rdb,
		presigner:     presigner,
		bucket:        bucket,
		endpoint:      endpoint,
		log:           log,
	}
}

// ListenAndNotify starts the Kafka consume loop for image.generated events.
// For every event it caches the preview URL in Redis and pushes a
// WSNotification to the connected client. It blocks until ctx is cancelled.
func (s *Service) ListenAndNotify(ctx context.Context) error {
	s.log.Info("Approval service consumer starting")
	return s.consumer.Consume(ctx, s.handleImageGenerated)
}

func (s *Service) handleImageGenerated(ctx context.Context, msg *kafka.Message) error {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "approval.HandleImageGenerated")
	defer span.End()

	var event model.ImageGeneratedEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		s.log.Error("Failed to unmarshal image.generated event",
			zap.Error(err),
			zap.String("topic", msg.Topic),
			zap.Int64("offset", msg.Offset),
		)
		return fmt.Errorf("unmarshal image.generated: %w", err)
	}

	span.SetAttributes(
		attribute.String("job.id", event.JobID),
		attribute.String("user.id", event.UserID),
	)

	s.log.Info("Processing image.generated event",
		zap.String("job_id", event.JobID),
		zap.String("user_id", event.UserID),
		zap.String("image_url", event.ImageURL),
	)

	previewURL := event.ImageURL
	if s.presigner != nil {
		key := storage.ExtractKeyFromURL(event.ImageURL, s.bucket, s.endpoint)
		signed, err := s.presigner.PresignGetObject(ctx, key, previewTTL)
		if err != nil {
			s.log.Warn("Presign failed, falling back to raw URL",
				zap.Error(err),
				zap.String("job_id", event.JobID),
			)
		} else {
			previewURL = signed
		}
	}

	preview := model.JobPreviewCache{
		SignedURL: previewURL,
		ImageURL:  event.ImageURL,
		UserID:    event.UserID,
		ExpiresAt: time.Now().Add(previewTTL),
	}

	previewJSON, err := json.Marshal(preview)
	if err != nil {
		return fmt.Errorf("marshal preview cache: %w", err)
	}

	if err := s.rdb.Set(ctx, previewKeyPrefix+event.JobID, previewJSON, previewTTL).Err(); err != nil {
		s.log.Error("Failed to cache preview in Redis",
			zap.Error(err),
			zap.String("job_id", event.JobID),
		)
		return fmt.Errorf("cache preview: %w", err)
	}

	notification := &approvalpb.JobUpdateNotification{
		JobId:            event.JobID,
		UserId:           event.UserID,
		Status:           model.JobStatusAwaitingApproval,
		Message:          "Image generated. Please review and approve or reject.",
		PreviewUrl:       previewURL,
		NotificationType: "preview_ready",
	}

	s.streamManager.Broadcast(notification)

	s.log.Info("Preview cached and client notified",
		zap.String("job_id", event.JobID),
		zap.String("preview_url", previewURL),
	)

	return nil
}

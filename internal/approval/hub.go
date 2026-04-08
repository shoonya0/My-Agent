package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"myAgent/pkg/kafka"
	"myAgent/pkg/model"
	apmotel "myAgent/pkg/otel"
	"myAgent/pkg/storage"
	ws "myAgent/pkg/websocket"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const (
	tracerName     = "internal/approval"
	topicApproved  = "image.approved"
	topicJobFailed = "job.failed"
	serviceName    = "approval-service"

	previewKeyPrefix = "job:preview:"
	sessionKeyPrefix = "ws:session:"
	previewTTL       = 1 * time.Hour
	sessionTTL       = 30 * time.Minute
)

// Service bridges Kafka image.generated events to WebSocket notifications
// and handles approve/reject user actions.
type Service struct {
	consumer  kafka.Consumer
	producer  kafka.Producer
	hub       *ws.Hub
	rdb       *redis.Client
	presigner storage.Presigner
	bucket    string
	endpoint  string
	log       *zap.Logger
}

// NewService constructs a Service with the required dependencies. presigner
// may be nil — in that case the raw object URL is used for previews.
func NewService(
	consumer kafka.Consumer,
	producer kafka.Producer,
	hub *ws.Hub,
	rdb *redis.Client,
	presigner storage.Presigner,
	bucket, endpoint string,
	log *zap.Logger,
) *Service {
	return &Service{
		consumer:  consumer,
		producer:  producer,
		hub:       hub,
		rdb:       rdb,
		presigner: presigner,
		bucket:    bucket,
		endpoint:  endpoint,
		log:       log,
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

	notification := model.WSNotification{
		Type:       "preview_ready",
		JobID:      event.JobID,
		Status:     model.JobStatusAwaitingApproval,
		PreviewURL: previewURL,
		Message:    "Image generated. Please review and approve or reject.",
	}

	if err := s.hub.SendToJob(event.JobID, notification); err != nil {
		s.log.Error("Failed to send WebSocket notification",
			zap.Error(err),
			zap.String("job_id", event.JobID),
		)
	}

	s.log.Info("Preview cached and client notified",
		zap.String("job_id", event.JobID),
		zap.String("preview_url", previewURL),
	)

	return nil
}

// Approve retrieves the cached preview and publishes an ImageApprovedEvent
// to the image.approved topic, triggering the distribution pipeline.
func (s *Service) Approve(ctx context.Context, jobID, userID string, req model.ApproveJobRequest) (*model.JobActionResponse, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "approval.Approve")
	defer span.End()
	span.SetAttributes(
		attribute.String("job.id", jobID),
		attribute.String("user.id", userID),
	)

	preview, err := s.getPreview(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("get preview for job %s: %w", jobID, err)
	}

	if preview.UserID != userID {
		s.log.Warn("Job ownership verification failed",
			zap.String("job_id", jobID),
			zap.String("expected_user", preview.UserID),
			zap.String("actual_user", userID),
		)
		return nil, fmt.Errorf("access denied: job %s does not belong to user %s", jobID, userID)
	}

	event := model.ImageApprovedEvent{
		JobID:     jobID,
		UserID:    userID,
		ImageURL:  preview.ImageURL,
		Caption:   req.Caption,
		Platforms: req.Platforms,
		TraceCtx:  apmotel.ExtractTraceContext(ctx),
	}

	if err := s.producer.Publish(ctx, topicApproved, jobID, event); err != nil {
		s.log.Error("Failed to publish image.approved",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
		return nil, fmt.Errorf("publish image.approved: %w", err)
	}

	notification := model.WSNotification{
		Type:    "job_update",
		JobID:   jobID,
		Status:  model.JobStatusDistributing,
		Message: "Image approved. Distribution started.",
	}
	_ = s.hub.SendToJob(jobID, notification)

	s.log.Info("Job approved",
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

// Reject marks the job as rejected, publishes a job.failed event, cleans up
// the cached preview, and notifies the client via WebSocket.
func (s *Service) Reject(ctx context.Context, jobID, userID string, req model.RejectJobRequest) (*model.JobActionResponse, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "approval.Reject")
	defer span.End()
	span.SetAttributes(
		attribute.String("job.id", jobID),
		attribute.String("user.id", userID),
	)

	reason := req.Reason
	if reason == "" {
		reason = "rejected by user"
	}

	s.publishFailure(ctx, jobID, userID, reason)

	notification := model.WSNotification{
		Type:    "job_update",
		JobID:   jobID,
		Status:  model.JobStatusRejected,
		Message: "Image rejected.",
	}
	_ = s.hub.SendToJob(jobID, notification)

	if err := s.rdb.Del(ctx, previewKeyPrefix+jobID).Err(); err != nil {
		s.log.Warn("Failed to delete preview cache",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
	}

	s.log.Info("Job rejected",
		zap.String("job_id", jobID),
		zap.String("user_id", userID),
		zap.String("reason", reason),
	)

	return &model.JobActionResponse{
		JobID:   jobID,
		Status:  model.JobStatusRejected,
		Message: "Image rejected.",
	}, nil
}

// RegisterWSSession stores a WebSocket session entry in Redis so that other
// nodes can locate the WebSocket handler for a given job.
func (s *Service) RegisterWSSession(ctx context.Context, jobID, userID, nodeID string) error {
	entry := model.WSSessionEntry{
		NodeID:      nodeID,
		UserID:      userID,
		ConnectedAt: time.Now(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal ws session: %w", err)
	}

	return s.rdb.Set(ctx, sessionKeyPrefix+jobID, data, sessionTTL).Err()
}

// RemoveWSSession deletes the WebSocket session entry from Redis.
func (s *Service) RemoveWSSession(ctx context.Context, jobID string) {
	if err := s.rdb.Del(ctx, sessionKeyPrefix+jobID).Err(); err != nil {
		s.log.Warn("Failed to remove WS session",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
	}
}

func (s *Service) getPreview(ctx context.Context, jobID string) (*model.JobPreviewCache, error) {
	data, err := s.rdb.Get(ctx, previewKeyPrefix+jobID).Bytes()
	if err != nil {
		return nil, fmt.Errorf("redis get preview: %w", err)
	}

	var preview model.JobPreviewCache
	if err := json.Unmarshal(data, &preview); err != nil {
		return nil, fmt.Errorf("unmarshal preview: %w", err)
	}

	return &preview, nil
}

func (s *Service) publishFailure(ctx context.Context, jobID, userID, errMsg string) {
	evt := model.JobFailedEvent{
		JobID:        jobID,
		UserID:       userID,
		FailedAt:     serviceName,
		ErrorMessage: errMsg,
		TraceCtx:     apmotel.ExtractTraceContext(ctx),
	}
	if err := s.producer.Publish(ctx, topicJobFailed, jobID, evt); err != nil {
		s.log.Error("Failed to publish job.failed event",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
	}
}

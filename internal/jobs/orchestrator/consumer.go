package orchestrator

import (
	"context"

	"myAgent/pkg/data/kafka"
	"myAgent/pkg/events"

	"go.uber.org/zap"
)

// JobFailedConsumerGroupID is the Kafka consumer group for job.failed in the orchestrator.
const JobFailedConsumerGroupID = "orchestrator-failed-consumer"

// NewJobFailedConsumer builds a consumer-group reader for the job.failed topic.
func NewJobFailedConsumer(brokers string, log *zap.Logger) (kafka.Consumer, error) {
	return kafka.NewConsumer(brokers, JobFailedConsumerGroupID, events.TopicJobFailed, log)
}

// RunJobFailedConsumer blocks until ctx is cancelled, delegating to Service.ConsumeJobFailedEvents.
func RunJobFailedConsumer(ctx context.Context, consumer kafka.Consumer, svc Service) error {
	return svc.ConsumeJobFailedEvents(ctx, consumer)
}

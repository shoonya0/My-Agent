package kafka

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

// Message represents a consumed Kafka record with its metadata and payload.
type Message struct {
	Topic     string
	Key       string
	Value     []byte
	Partition int32
	Offset    int64
}

// MessageHandler processes a single consumed Kafka message. Returning an error
// does not prevent offset commit (sarama auto-marks after handler returns) but
// lets the caller log and publish failure events.
type MessageHandler func(ctx context.Context, msg *Message) error

// Consumer reads messages from a Kafka topic within a consumer group.
type Consumer interface {
	// Consume blocks until ctx is cancelled, calling handler for every message.
	Consume(ctx context.Context, handler MessageHandler) error
	Close() error
}

type saramaConsumer struct {
	group   sarama.ConsumerGroup
	topic   string
	handler MessageHandler
	log     *zap.Logger
	ready   chan struct{}
}

// NewConsumer creates a consumer-group backed Kafka Consumer. groupID
// determines offset tracking; topic is the single topic to subscribe to.
func NewConsumer(brokers, groupID, topic string, log *zap.Logger) (Consumer, error) {
	cfg := sarama.NewConfig()
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		sarama.NewBalanceStrategyRoundRobin(),
	}
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Consumer.Return.Errors = true

	addrs := parseBrokers(brokers)

	group, err := sarama.NewConsumerGroup(addrs, groupID, cfg)
	if err != nil {
		return nil, fmt.Errorf("kafka: create consumer group: %w", err)
	}

	log.Info("Kafka consumer group created",
		zap.String("group_id", groupID),
		zap.String("topic", topic),
		zap.Strings("brokers", addrs),
	)

	return &saramaConsumer{
		group: group,
		topic: topic,
		log:   log,
		ready: make(chan struct{}),
	}, nil
}

// Consume blocks and reads messages until ctx is cancelled. It re-enters the
// consumer-group session loop after every rebalance.
func (c *saramaConsumer) Consume(ctx context.Context, handler MessageHandler) error {
	c.handler = handler

	go func() {
		for err := range c.group.Errors() {
			c.log.Error("Kafka consumer error", zap.Error(err))
		}
	}()

	for {
		if err := c.group.Consume(ctx, []string{c.topic}, c); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("kafka: consume session: %w", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		c.ready = make(chan struct{})
	}
}

func (c *saramaConsumer) Close() error {
	return c.group.Close()
}

// --- sarama.ConsumerGroupHandler interface ---

func (c *saramaConsumer) Setup(_ sarama.ConsumerGroupSession) error {
	close(c.ready)
	return nil
}

func (c *saramaConsumer) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

func (c *saramaConsumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}

			ctx := extractTraceContext(context.Background(), msg.Headers)

			m := &Message{
				Topic:     msg.Topic,
				Key:       string(msg.Key),
				Value:     msg.Value,
				Partition: msg.Partition,
				Offset:    msg.Offset,
			}

			if err := c.handler(ctx, m); err != nil {
				c.log.Error("Message handler failed",
					zap.String("topic", msg.Topic),
					zap.Int32("partition", msg.Partition),
					zap.Int64("offset", msg.Offset),
					zap.Error(err),
				)
			}

			session.MarkMessage(msg, "")

		case <-session.Context().Done():
			return nil
		}
	}
}

// extractTraceContext rebuilds the OTel context from Kafka record headers,
// allowing downstream spans to be parented to the producer's trace.
func extractTraceContext(ctx context.Context, headers []*sarama.RecordHeader) context.Context {
	carrier := propagation.MapCarrier{}
	for _, h := range headers {
		carrier.Set(string(h.Key), string(h.Value))
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

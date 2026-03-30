package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

// Producer publishes JSON-encoded messages to Kafka topics with OTel trace
// propagation via record headers.
type Producer interface {
	Publish(ctx context.Context, topic, key string, value any) error
	Close() error
}

type saramaProducer struct {
	producer sarama.SyncProducer
	log      *zap.Logger
}

// NewProducer creates a synchronous Kafka producer connected to the given
// comma-separated broker addresses. It uses WaitForAll acks and retries
// up to 3 times on transient failures.
func NewProducer(brokers string, log *zap.Logger) (Producer, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 3
	cfg.Producer.Return.Successes = true

	addrs := parseBrokers(brokers)

	producer, err := sarama.NewSyncProducer(addrs, cfg)
	if err != nil {
		return nil, fmt.Errorf("kafka: create producer: %w", err)
	}

	log.Info("Kafka producer connected", zap.Strings("brokers", addrs))
	return &saramaProducer{producer: producer, log: log}, nil
}

// Publish serialises value as JSON and sends it to the given topic with OTel
// trace headers attached. The key is used for partition assignment.
func (p *saramaProducer) Publish(ctx context.Context, topic, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("kafka: marshal message: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic:   topic,
		Key:     sarama.StringEncoder(key),
		Value:   sarama.ByteEncoder(data),
		Headers: injectTraceHeaders(ctx),
	}

	partition, offset, err := p.producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("kafka: publish to %s: %w", topic, err)
	}

	p.log.Debug("Published Kafka message",
		zap.String("topic", topic),
		zap.String("key", key),
		zap.Int32("partition", partition),
		zap.Int64("offset", offset),
	)
	return nil
}

func (p *saramaProducer) Close() error {
	return p.producer.Close()
}

func parseBrokers(raw string) []string {
	parts := strings.Split(raw, ",")
	addrs := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			addrs = append(addrs, s)
		}
	}
	return addrs
}

// injectTraceHeaders extracts the OTel trace context from ctx and returns
// Kafka record headers so downstream consumers can continue the trace.
func injectTraceHeaders(ctx context.Context) []sarama.RecordHeader {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	headers := make([]sarama.RecordHeader, 0, len(carrier))
	for k, v := range carrier {
		headers = append(headers, sarama.RecordHeader{
			Key:   []byte(k),
			Value: []byte(v),
		})
	}
	return headers
}

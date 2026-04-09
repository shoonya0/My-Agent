package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"myAgent/pkg/types"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const telegramAPIBase = "https://api.telegram.org/bot"

// Telegram publishes images via the Telegram Bot API sendPhoto method.
// Requires metadata keys telegram_token and telegram_chat_id on every PostRequest.
type Telegram struct {
	client *http.Client
}

// NewTelegram creates a stateless Telegram connector.
func NewTelegram() *Telegram {
	return &Telegram{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *Telegram) Name() string { return "telegram" }

func (t *Telegram) Validate(context.Context) error { return nil }

func (t *Telegram) Publish(ctx context.Context, req types.PostRequest) (*types.PublishResult, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "telegram.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("platform", "telegram"))

	token := req.Metadata["telegram_token"]
	if token == "" {
		err := fmt.Errorf("telegram: metadata must include telegram_token")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	chatID := req.Metadata["telegram_chat_id"]
	if chatID == "" {
		err := fmt.Errorf("telegram: metadata must include telegram_chat_id")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	body := map[string]any{
		"chat_id": chatID,
		"photo":   req.MediaURL,
		"caption": req.Caption,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request payload: %w", err)
	}
	endpoint := fmt.Sprintf("%s%s/sendPhoto", telegramAPIBase, token)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("telegram: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("telegram: http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("telegram: unexpected status %d: %s", resp.StatusCode, respBody)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram: decode response: %w", err)
	}

	span.SetStatus(codes.Ok, "published")
	return &types.PublishResult{
		PlatformPostID: fmt.Sprintf("%d", result.Result.MessageID),
	}, nil
}

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

// Discord publishes images to a Discord channel via webhook URL in
// PostRequest.Metadata["discord_webhook_url"].
type Discord struct {
	client *http.Client
}

// NewDiscord creates a stateless Discord connector.
func NewDiscord() *Discord {
	return &Discord{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (d *Discord) Name() string { return "discord" }

func (d *Discord) Validate(context.Context) error { return nil }

func (d *Discord) Publish(ctx context.Context, req types.PostRequest) (*types.PublishResult, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "discord.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("platform", "discord"))

	webhookURL := req.Metadata["discord_webhook_url"]
	if webhookURL == "" {
		err := fmt.Errorf("discord: metadata must include discord_webhook_url")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	body := map[string]any{
		"content": req.Caption,
		"embeds": []map[string]any{
			{
				"image": map[string]string{
					"url": req.MediaURL,
				},
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		err = fmt.Errorf("discord: marshal request payload: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	webhookURLWithWait := webhookURL + "?wait=true"
	// ?wait=true makes Discord return the created message object.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURLWithWait, bytes.NewReader(payload))
	if err != nil {
		err = fmt.Errorf("discord: build request: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	span.SetAttributes(
		attribute.String("http.request.method", "POST"),
		attribute.String("http.url", webhookURLWithWait),
	)

	resp, err := d.client.Do(httpReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("discord: http call: %w", err)
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("discord: unexpected status %d: %s", resp.StatusCode, respBody)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	var result struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		err = fmt.Errorf("discord: decode response: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetStatus(codes.Ok, "published")
	return &types.PublishResult{
		PlatformPostID: result.ID,
		PlatformURL:    fmt.Sprintf("https://discord.com/channels/@me/%s/%s", result.ChannelID, result.ID),
	}, nil
}

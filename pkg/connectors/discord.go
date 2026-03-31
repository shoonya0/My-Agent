package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"myAgent/pkg/model"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Discord publishes images to a Discord channel via webhook. An optional
// per-request override can be passed in metadata["discord_webhook_url"].
type Discord struct {
	webhookURL string
	client     *http.Client
}

// NewDiscord creates a Discord connector that posts to the given webhook URL.
func NewDiscord(webhookURL string) *Discord {
	return &Discord{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (d *Discord) Name() string { return "discord" }

func (d *Discord) Validate(_ context.Context) error {
	if d.webhookURL == "" {
		return fmt.Errorf("discord: webhook URL is not configured")
	}
	return nil
}

func (d *Discord) Publish(ctx context.Context, req model.PostRequest) (*model.PublishResult, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "discord.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("platform", "discord"))

	webhookURL := d.webhookURL
	if override := req.Metadata["discord_webhook_url"]; override != "" {
		webhookURL = override
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

	payload, _ := json.Marshal(body)

	// ?wait=true makes Discord return the created message object.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL+"?wait=true", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("discord: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(httpReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("discord: http call: %w", err)
	}
	defer resp.Body.Close()

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
		return nil, fmt.Errorf("discord: decode response: %w", err)
	}

	span.SetStatus(codes.Ok, "published")
	return &model.PublishResult{
		PlatformPostID: result.ID,
		PlatformURL:    fmt.Sprintf("https://discord.com/channels/@me/%s/%s", result.ChannelID, result.ID),
	}, nil
}

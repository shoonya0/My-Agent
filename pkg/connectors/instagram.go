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

const instagramAPIBase = "https://graph.instagram.com/v21.0/me"

// Instagram publishes images via the Instagram Graph API using a two-step
// flow: create a media container, then publish it.
type Instagram struct {
	token  string
	client *http.Client
}

// NewInstagram creates an Instagram connector authenticated with the given
// long-lived page access token.
func NewInstagram(token string) *Instagram {
	return &Instagram{
		token:  token,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (ig *Instagram) Name() string { return "instagram" }

func (ig *Instagram) Validate(_ context.Context) error {
	if ig.token == "" {
		return fmt.Errorf("instagram: access token is not configured")
	}
	return nil
}

func (ig *Instagram) Publish(ctx context.Context, req model.PostRequest) (*model.PublishResult, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "instagram.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("platform", "instagram"))

	containerID, err := ig.createMediaContainer(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("instagram: create media container: %w", err)
	}

	publishID, err := ig.publishContainer(ctx, containerID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("instagram: publish container: %w", err)
	}

	span.SetStatus(codes.Ok, "published")
	return &model.PublishResult{
		PlatformPostID: publishID,
		PlatformURL:    fmt.Sprintf("https://www.instagram.com/p/%s", publishID),
	}, nil
}

func (ig *Instagram) createMediaContainer(ctx context.Context, req model.PostRequest) (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"image_url":    req.MediaURL,
		"caption":      req.Caption,
		"access_token": ig.token,
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, instagramAPIBase+"/media", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := ig.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.ID, nil
}

func (ig *Instagram) publishContainer(ctx context.Context, containerID string) (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"creation_id":  containerID,
		"access_token": ig.token,
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, instagramAPIBase+"/media_publish", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := ig.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.ID, nil
}

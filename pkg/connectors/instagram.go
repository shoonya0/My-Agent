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

const instagramAPIBase = "https://graph.instagram.com/v21.0/me"

// Instagram publishes images via the Instagram Graph API using a two-step
// flow: create a media container, then publish it. Authentication is per
// request via PostRequest.Metadata["instagram_token"].
type Instagram struct {
	client *http.Client
}

// NewInstagram creates a stateless Instagram connector.
func NewInstagram() *Instagram {
	return &Instagram{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (ig *Instagram) Name() string { return "instagram" }

func (ig *Instagram) Validate(context.Context) error { return nil }

func (ig *Instagram) Publish(ctx context.Context, req types.PostRequest) (*types.PublishResult, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "instagram.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("platform", "instagram"))

	token := req.Metadata["instagram_token"]
	if token == "" {
		err := fmt.Errorf("instagram: metadata must include instagram_token")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	containerID, err := ig.createMediaContainer(ctx, req, token)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("instagram: create media container: %w", err)
	}

	publishID, err := ig.publishContainer(ctx, containerID, token)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("instagram: publish container: %w", err)
	}

	span.SetStatus(codes.Ok, "published")
	return &types.PublishResult{
		PlatformPostID: publishID,
		PlatformURL:    fmt.Sprintf("https://www.instagram.com/p/%s", publishID),
	}, nil
}

func (ig *Instagram) createMediaContainer(ctx context.Context, req types.PostRequest, token string) (string, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "instagram.createMediaContainer")
	defer span.End()

	payload, err := json.Marshal(map[string]string{
		"image_url":    req.MediaURL,
		"caption":      req.Caption,
		"access_token": token,
	})
	if err != nil {
		err = fmt.Errorf("instagram: marshal request payload: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	endpoint := instagramAPIBase + "/media"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		err = fmt.Errorf("instagram: build request: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	span.SetAttributes(
		attribute.String("http.request.method", "POST"),
		attribute.String("http.url", endpoint),
	)

	resp, err := ig.client.Do(httpReq)
	if err != nil {
		err = fmt.Errorf("instagram: http call: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("instagram: unexpected status %d: %s", resp.StatusCode, body)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		err = fmt.Errorf("instagram: decode response: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	span.SetStatus(codes.Ok, "container created")
	return result.ID, nil
}

func (ig *Instagram) publishContainer(ctx context.Context, containerID string, token string) (string, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "instagram.publishContainer")
	defer span.End()

	payload, err := json.Marshal(map[string]string{
		"creation_id":  containerID,
		"access_token": token,
	})
	if err != nil {
		err = fmt.Errorf("instagram: marshal request payload: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	endpoint := instagramAPIBase + "/media_publish"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		err = fmt.Errorf("instagram: build request: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	span.SetAttributes(
		attribute.String("http.request.method", "POST"),
		attribute.String("http.url", endpoint),
	)

	resp, err := ig.client.Do(httpReq)
	if err != nil {
		err = fmt.Errorf("instagram: http call: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("instagram: unexpected status %d: %s", resp.StatusCode, body)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		err = fmt.Errorf("instagram: decode response: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	span.SetStatus(codes.Ok, "published")
	return result.ID, nil
}

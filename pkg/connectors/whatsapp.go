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

const whatsappAPIBase = "https://graph.facebook.com/v21.0"

// WhatsApp publishes images via the WhatsApp Business Cloud API.
// Requires metadata keys whatsapp_token, whatsapp_phone_number_id, and
// whatsapp_recipient on every PostRequest.
type WhatsApp struct {
	client *http.Client
}

// NewWhatsApp creates a stateless WhatsApp connector.
func NewWhatsApp() *WhatsApp {
	return &WhatsApp{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (wa *WhatsApp) Name() string { return "whatsapp" }

func (wa *WhatsApp) Validate(context.Context) error { return nil }

func (wa *WhatsApp) Publish(ctx context.Context, req types.PostRequest) (*types.PublishResult, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "whatsapp.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("platform", "whatsapp"))

	token := req.Metadata["whatsapp_token"]
	if token == "" {
		err := fmt.Errorf("whatsapp: metadata must include whatsapp_token")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	phoneNumberID := req.Metadata["whatsapp_phone_number_id"]
	recipientPhone := req.Metadata["whatsapp_recipient"]
	if phoneNumberID == "" || recipientPhone == "" {
		err := fmt.Errorf("whatsapp: metadata must include whatsapp_phone_number_id and whatsapp_recipient")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	body := map[string]any{
		"messaging_product": "whatsapp",
		"to":                recipientPhone,
		"type":              "image",
		"image": map[string]string{
			"link":    req.MediaURL,
			"caption": req.Caption,
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request payload: %w", err)
	}
	endpoint := fmt.Sprintf("%s/%s/messages", whatsappAPIBase, phoneNumberID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("whatsapp: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := wa.client.Do(httpReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("whatsapp: http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("whatsapp: unexpected status %d: %s", resp.StatusCode, respBody)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	var result struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("whatsapp: decode response: %w", err)
	}

	messageID := ""
	if len(result.Messages) > 0 {
		messageID = result.Messages[0].ID
	}

	span.SetStatus(codes.Ok, "published")
	return &types.PublishResult{PlatformPostID: messageID}, nil
}

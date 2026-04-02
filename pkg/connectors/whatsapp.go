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

const whatsappAPIBase = "https://graph.facebook.com/v21.0"

// WhatsApp publishes images via the WhatsApp Business Cloud API.
// Requires metadata keys "whatsapp_phone_number_id" and "whatsapp_recipient"
// on every PostRequest.
type WhatsApp struct {
	token  string
	client *http.Client
}

// NewWhatsApp creates a WhatsApp connector using the given API bearer token.
func NewWhatsApp(token string) *WhatsApp {
	return &WhatsApp{
		token:  token,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (wa *WhatsApp) Name() string { return "whatsapp" }

func (wa *WhatsApp) Validate(_ context.Context) error {
	if wa.token == "" {
		return fmt.Errorf("whatsapp: bearer token is not configured")
	}
	return nil
}

func (wa *WhatsApp) Publish(ctx context.Context, req model.PostRequest) (*model.PublishResult, error) {
	ctx, span := otel.Tracer("pkg/connectors").Start(ctx, "whatsapp.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("platform", "whatsapp"))

	token := wa.token
	if override := req.Metadata["whatsapp_token"]; override != "" {
		token = override
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

	payload, _ := json.Marshal(body)
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
	return &model.PublishResult{PlatformPostID: messageID}, nil
}

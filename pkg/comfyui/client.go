package comfyui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"myAgent/pkg/types"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const tracerName = "pkg/comfyui"

// GenerationResult holds the output of a successful ComfyUI generation.
type GenerationResult struct {
	PromptID string
	Images   []OutputImage
	Duration time.Duration
}

// OutputImage represents a single generated image from ComfyUI.
type OutputImage struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

// Client communicates with a ComfyUI instance over HTTP to queue workflows,
// poll for completion, and download generated images.
type Client interface {
	QueuePrompt(ctx context.Context, input *types.ComfyWorkflowInput) (promptID string, err error)
	WaitForResult(ctx context.Context, promptID string) (*GenerationResult, error)
	DownloadImage(ctx context.Context, filename, subfolder string) ([]byte, error)
}

type comfyClient struct {
	baseURL    string
	httpClient *http.Client
	log        *zap.Logger
}

// NewClient creates a ComfyUI HTTP client targeting the given base URL.
func NewClient(baseURL string, log *zap.Logger) Client {
	return &comfyClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		log: log,
	}
}

type promptResponse struct {
	PromptID string `json:"prompt_id"`
}

// QueuePrompt submits a workflow to ComfyUI and returns the assigned prompt ID.
func (c *comfyClient) QueuePrompt(ctx context.Context, input *types.ComfyWorkflowInput) (string, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "comfyui.QueuePrompt")
	defer span.End()

	body, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("comfyui: marshal workflow: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/prompt", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("comfyui: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("comfyui: queue prompt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("comfyui: queue prompt: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result promptResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("comfyui: decode prompt response: %w", err)
	}

	span.SetAttributes(attribute.String("comfyui.prompt_id", result.PromptID))
	c.log.Debug("ComfyUI prompt queued", zap.String("prompt_id", result.PromptID))

	return result.PromptID, nil
}

// WaitForResult polls ComfyUI's /history endpoint until the prompt completes.
// Uses exponential backoff starting at 2 s, capped at 30 s, with a 10-minute
// hard deadline.
func (c *comfyClient) WaitForResult(ctx context.Context, promptID string) (*GenerationResult, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "comfyui.WaitForResult")
	defer span.End()
	span.SetAttributes(attribute.String("comfyui.prompt_id", promptID))

	const (
		initialBackoff = 2 * time.Second
		maxBackoff     = 30 * time.Second
		timeout        = 10 * time.Minute
	)

	start := time.Now()
	deadline := time.After(timeout)
	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, fmt.Errorf("comfyui: generation timed out after %v for prompt %s", timeout, promptID)
		default:
		}

		images, done, err := c.checkHistory(ctx, promptID)
		if err != nil {
			c.log.Warn("ComfyUI history check failed, retrying",
				zap.String("prompt_id", promptID),
				zap.Error(err),
			)
		} else if done {
			duration := time.Since(start)
			span.SetAttributes(attribute.Int64("comfyui.generation_ms", duration.Milliseconds()))
			return &GenerationResult{
				PromptID: promptID,
				Images:   images,
				Duration: duration,
			}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

// historyEntry models the relevant portion of a single prompt's history.
type historyEntry struct {
	Outputs map[string]struct {
		Images []OutputImage `json:"images"`
	} `json:"outputs"`
}

func (c *comfyClient) checkHistory(ctx context.Context, promptID string) ([]OutputImage, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/history/"+promptID, nil)
	if err != nil {
		return nil, false, fmt.Errorf("comfyui: create history request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("comfyui: check history: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("comfyui: history status %d", resp.StatusCode)
	}

	var history map[string]historyEntry
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		return nil, false, fmt.Errorf("comfyui: decode history: %w", err)
	}

	entry, ok := history[promptID]
	if !ok {
		return nil, false, nil
	}

	var images []OutputImage
	for _, output := range entry.Outputs {
		images = append(images, output.Images...)
	}

	if len(images) == 0 {
		return nil, false, nil
	}

	return images, true, nil
}

// DownloadImage fetches a generated image from ComfyUI's /view endpoint.
func (c *comfyClient) DownloadImage(ctx context.Context, filename, subfolder string) ([]byte, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "comfyui.DownloadImage")
	defer span.End()

	params := url.Values{}
	params.Set("filename", filename)
	if subfolder != "" {
		params.Set("subfolder", subfolder)
	}
	params.Set("type", "output")

	reqURL := fmt.Sprintf("%s/view?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("comfyui: create view request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("comfyui: download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comfyui: download image: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("comfyui: read image body: %w", err)
	}

	span.SetAttributes(attribute.Int("comfyui.image_bytes", len(data)))
	return data, nil
}

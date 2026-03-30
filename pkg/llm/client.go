package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"myAgent/pkg/model"

	openai "github.com/sashabaranov/go-openai"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const tracerName = "pkg/llm"

// Client abstracts LLM operations used by the orchestrator to parse user
// intent into a structured ExecutionPlan.
type Client interface {
	ParseIntent(ctx context.Context, prompt string) (*model.ExecutionPlan, error)
}

type openAIClient struct {
	client *openai.Client
	model  string
	log    *zap.Logger
}

// NewClient constructs an LLM Client backed by the OpenAI chat completions
// API. modelName should be a valid model identifier (e.g. "gpt-4o").
func NewClient(apiKey, modelName string, log *zap.Logger) Client {
	return &openAIClient{
		client: openai.NewClient(apiKey),
		model:  modelName,
		log:    log,
	}
}

const systemPrompt = `You are an image-editing intent parser. Given a user's natural-language prompt describing desired edits to an image, produce a JSON ExecutionPlan.

Output ONLY valid JSON matching this schema:
{
  "edits": [{"operation": "add_element|remove|relight|recolor", "target": "background|sky|subject|...", "description": "...", "priority": 1}],
  "style": {"lighting_temp": "warm|cold|neutral", "angle_degrees": 0.0, "depth_of_field": "shallow|deep", "mood": "...", "style_preset": "..."},
  "background_replace": false,
  "subject_preserve": true,
  "mood": "..."
}

Rules:
- Assign priority starting from 1 (highest).
- Infer style parameters from the prompt; use sensible defaults for missing values.
- Keep descriptions concise and actionable for a ComfyUI image pipeline.`

// ParseIntent sends the user's prompt to the LLM and parses the structured
// JSON response into an ExecutionPlan. Returns an error if the LLM call fails
// or the response cannot be deserialised.
func (c *openAIClient) ParseIntent(ctx context.Context, prompt string) (*model.ExecutionPlan, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "llm.ParseIntent")
	defer span.End()

	span.SetAttributes(
		attribute.String("llm.model", c.model),
		attribute.Int("llm.prompt_len", len(prompt)),
	)

	start := time.Now()

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.2,
	})

	durationMs := time.Since(start).Milliseconds()
	span.SetAttributes(attribute.Int64("llm.duration_ms", durationMs))

	if err != nil {
		return nil, fmt.Errorf("llm: chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty response from model %s", c.model)
	}

	raw := resp.Choices[0].Message.Content
	c.log.Debug("LLM response received",
		zap.Int64("duration_ms", durationMs),
		zap.Int("response_len", len(raw)),
	)

	var plan model.ExecutionPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, fmt.Errorf("llm: unmarshal execution plan: %w", err)
	}

	return &plan, nil
}

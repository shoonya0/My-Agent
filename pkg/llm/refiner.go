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

// PromptRefiner rewrites a user's raw prompt into a detailed, ComfyUI-ready
// prompt and extracts structured style parameters via an LLM call.
type PromptRefiner interface {
	RefinePrompt(ctx context.Context, original string, plan model.ExecutionPlan) (*RefinedOutput, error)
}

// RefinedOutput holds the LLM's structured response for prompt refinement.
type RefinedOutput struct {
	Prompt      string           `json:"prompt"`
	StyleParams model.StyleParams `json:"style_params"`
}

type openAIRefiner struct {
	client       *openai.Client
	model        string
	systemPrompt string
	log          *zap.Logger
}

const defaultRefinerPrompt = `You are an expert image-editing prompt engineer. Given a user's original prompt and an ExecutionPlan describing the desired edits, rewrite the prompt into a detailed, precise instruction suitable for a ComfyUI image-generation pipeline.

Output ONLY valid JSON matching this schema:
{
  "prompt": "A detailed, rewritten prompt optimized for ComfyUI...",
  "style_params": {
    "lighting_temp": "warm|cold|neutral",
    "angle_degrees": 0.0,
    "depth_of_field": "shallow|deep",
    "mood": "...",
    "style_preset": "..."
  }
}

Rules:
- Make the rewritten prompt vivid, specific, and actionable.
- Incorporate all edit instructions from the ExecutionPlan with appropriate detail.
- Infer style parameters from context; use sensible defaults for missing values.
- Preserve the user's creative intent while adding technical precision.
- Keep the prompt concise but comprehensive (under 500 words).`

// NewRefiner constructs a PromptRefiner backed by the OpenAI chat API.
// If systemPrompt is empty, a sensible default is used — this allows the
// rewriting persona to be configured without code changes.
func NewRefiner(apiKey, modelName, systemPrompt string, log *zap.Logger) PromptRefiner {
	if systemPrompt == "" {
		systemPrompt = defaultRefinerPrompt
	}
	return &openAIRefiner{
		client:       openai.NewClient(apiKey),
		model:        modelName,
		systemPrompt: systemPrompt,
		log:          log,
	}
}

// RefinePrompt sends the original prompt and execution plan to the LLM and
// returns a refined prompt with extracted style parameters.
func (r *openAIRefiner) RefinePrompt(ctx context.Context, original string, plan model.ExecutionPlan) (*RefinedOutput, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "llm.RefinePrompt")
	defer span.End()

	span.SetAttributes(
		attribute.String("llm.model", r.model),
		attribute.Int("llm.prompt_len", len(original)),
	)

	planJSON, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal execution plan: %w", err)
	}

	userMsg := fmt.Sprintf("Original prompt: %s\n\nExecution plan:\n%s", original, string(planJSON))

	start := time.Now()

	resp, err := r.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: r.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: r.systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userMsg},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.4,
	})

	durationMs := time.Since(start).Milliseconds()
	span.SetAttributes(attribute.Int64("llm.duration_ms", durationMs))

	if err != nil {
		return nil, fmt.Errorf("llm: chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty response from model %s", r.model)
	}

	raw := resp.Choices[0].Message.Content
	r.log.Debug("LLM refine response received",
		zap.Int64("duration_ms", durationMs),
		zap.Int("response_len", len(raw)),
	)

	var output RefinedOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, fmt.Errorf("llm: unmarshal refined output: %w", err)
	}

	return &output, nil
}

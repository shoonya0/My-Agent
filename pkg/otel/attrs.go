package otel

import "go.opentelemetry.io/otel/attribute"

// Standard span attribute keys used across all services. Defined as constants
// so every service tags spans with identical keys — essential for consistent
// querying in Jaeger.
const (
	AttrJobID         = attribute.Key("job.id")
	AttrUserID        = attribute.Key("user.id")
	AttrKafkaTopic    = attribute.Key("kafka.topic")
	AttrLLMModel      = attribute.Key("llm.model")
	AttrLLMDurationMs = attribute.Key("llm.duration_ms")
	AttrComfyPromptID = attribute.Key("comfyui.prompt_id")
)

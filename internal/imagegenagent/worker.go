package imagegenagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"time"

	"myAgent/pkg/comfyui"
	"myAgent/pkg/kafka"
	"myAgent/pkg/model"
	"myAgent/pkg/storage"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

const (
	tracerName     = "internal/imagegenagent"
	topicGenerated = "image.generated"
	topicJobFailed = "job.failed"
	serviceName    = "image-gen-agent"
)

// Worker consumes prompt.refined events, generates images via ComfyUI,
// uploads them to S3, and publishes image.generated events. On failure it
// publishes a job.failed event so the orchestrator can mark the job.
type Worker struct {
	consumer kafka.Consumer
	producer kafka.Producer
	comfy    comfyui.Client
	uploader storage.Uploader
	log      *zap.Logger
}

// NewWorker constructs a Worker with the required dependencies.
func NewWorker(consumer kafka.Consumer, producer kafka.Producer, comfy comfyui.Client, uploader storage.Uploader, log *zap.Logger) *Worker {
	return &Worker{
		consumer: consumer,
		producer: producer,
		comfy:    comfy,
		uploader: uploader,
		log:      log,
	}
}

// Run starts the Kafka consume loop. It blocks until ctx is cancelled,
// processing each RefinedPromptEvent through the ComfyUI pipeline.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("Image-gen-agent worker starting")
	return w.consumer.Consume(ctx, w.handle)
}

func (w *Worker) handle(ctx context.Context, msg *kafka.Message) error {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "imagegenagent.HandleMessage")
	defer span.End()

	var event model.RefinedPromptEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		w.log.Error("Failed to unmarshal refined prompt event",
			zap.Error(err),
			zap.String("topic", msg.Topic),
			zap.Int64("offset", msg.Offset),
		)
		return fmt.Errorf("unmarshal refined prompt event: %w", err)
	}

	span.SetAttributes(
		attribute.String("job.id", event.JobID),
		attribute.String("user.id", event.UserID),
	)

	w.log.Info("Processing image generation",
		zap.String("job_id", event.JobID),
		zap.String("user_id", event.UserID),
	)

	workflow := buildWorkflow(event.RefinedPrompt, event.StyleParams, event.OriginalImageURL)

	promptID, err := w.comfy.QueuePrompt(ctx, workflow)
	if err != nil {
		w.log.Error("Failed to queue ComfyUI prompt",
			zap.Error(err),
			zap.String("job_id", event.JobID),
		)
		w.publishFailure(ctx, event.JobID, event.UserID, fmt.Sprintf("queue comfyui prompt: %v", err))
		return fmt.Errorf("queue comfyui prompt for job %s: %w", event.JobID, err)
	}

	span.SetAttributes(attribute.String("comfyui.prompt_id", promptID))

	result, err := w.comfy.WaitForResult(ctx, promptID)
	if err != nil {
		w.log.Error("ComfyUI generation failed",
			zap.Error(err),
			zap.String("job_id", event.JobID),
			zap.String("prompt_id", promptID),
		)
		w.publishFailure(ctx, event.JobID, event.UserID, fmt.Sprintf("comfyui generation: %v", err))
		return fmt.Errorf("comfyui generation for job %s: %w", event.JobID, err)
	}

	if len(result.Images) == 0 {
		errMsg := "comfyui returned no images"
		w.log.Error(errMsg, zap.String("job_id", event.JobID))
		w.publishFailure(ctx, event.JobID, event.UserID, errMsg)
		return fmt.Errorf("%s for job %s", errMsg, event.JobID)
	}

	img := result.Images[0]
	imageData, err := w.comfy.DownloadImage(ctx, img.Filename, img.Subfolder)
	if err != nil {
		w.log.Error("Failed to download generated image",
			zap.Error(err),
			zap.String("job_id", event.JobID),
			zap.String("filename", img.Filename),
		)
		w.publishFailure(ctx, event.JobID, event.UserID, fmt.Sprintf("download image: %v", err))
		return fmt.Errorf("download image for job %s: %w", event.JobID, err)
	}

	key := fmt.Sprintf("generated/%s/%s%s", event.JobID, uuid.New().String(), path.Ext(img.Filename))
	contentType := http.DetectContentType(imageData)

	imageURL, err := w.uploader.Upload(ctx, key, imageData, contentType)
	if err != nil {
		w.log.Error("Failed to upload image to S3",
			zap.Error(err),
			zap.String("job_id", event.JobID),
			zap.String("key", key),
		)
		w.publishFailure(ctx, event.JobID, event.UserID, fmt.Sprintf("upload to s3: %v", err))
		return fmt.Errorf("upload image for job %s: %w", event.JobID, err)
	}

	genEvent := model.ImageGeneratedEvent{
		JobID:         event.JobID,
		UserID:        event.UserID,
		ImageURL:      imageURL,
		ComfyPromptID: promptID,
		GenerationMs:  result.Duration.Milliseconds(),
		TraceCtx:      extractTraceCtx(ctx),
	}

	if err := w.producer.Publish(ctx, topicGenerated, event.JobID, genEvent); err != nil {
		w.log.Error("Failed to publish image.generated event",
			zap.Error(err),
			zap.String("job_id", event.JobID),
		)
		w.publishFailure(ctx, event.JobID, event.UserID, fmt.Sprintf("publish generated event: %v", err))
		return fmt.Errorf("publish generated event for job %s: %w", event.JobID, err)
	}

	w.log.Info("Image generated and published",
		zap.String("job_id", event.JobID),
		zap.String("prompt_id", promptID),
		zap.Int64("generation_ms", result.Duration.Milliseconds()),
		zap.String("image_url", imageURL),
	)

	return nil
}

// publishFailure sends a job.failed event so the orchestrator can mark the
// job as failed. Errors are logged but not returned to avoid masking the
// root cause.
func (w *Worker) publishFailure(ctx context.Context, jobID, userID, errMsg string) {
	evt := model.JobFailedEvent{
		JobID:        jobID,
		UserID:       userID,
		FailedAt:     serviceName,
		ErrorMessage: errMsg,
		TraceCtx:     extractTraceCtx(ctx),
	}
	if err := w.producer.Publish(ctx, topicJobFailed, jobID, evt); err != nil {
		w.log.Error("Failed to publish job.failed event",
			zap.Error(err),
			zap.String("job_id", jobID),
		)
	}
}

// buildWorkflow constructs a ComfyUI workflow from the refined prompt and
// style parameters. This produces a standard img2img pipeline; for custom
// workflows, replace this with a JSON-template–based builder.
func buildWorkflow(prompt string, style model.StyleParams, imageURL string) *model.ComfyWorkflowInput {
	clientID := uuid.New().String()

	nodes := map[string]model.ComfyNode{
		"1": {
			ClassType: "LoadImage",
			Inputs: map[string]any{
				"image": imageURL,
			},
		},
		"2": {
			ClassType: "CLIPTextEncode",
			Inputs: map[string]any{
				"text": prompt,
				"clip": []any{"4", 1},
			},
		},
		"3": {
			ClassType: "CLIPTextEncode",
			Inputs: map[string]any{
				"text": "",
				"clip": []any{"4", 1},
			},
		},
		"4": {
			ClassType: "CheckpointLoaderSimple",
			Inputs: map[string]any{
				"ckpt_name": resolveCheckpoint(style.StylePreset),
			},
		},
		"5": {
			ClassType: "KSampler",
			Inputs: map[string]any{
				"model":        []any{"4", 0},
				"positive":     []any{"2", 0},
				"negative":     []any{"3", 0},
				"latent_image": []any{"6", 0},
				"seed":         time.Now().UnixNano(),
				"steps":        30,
				"cfg":          7.0,
				"sampler_name": "euler_ancestral",
				"scheduler":    "normal",
				"denoise":      0.65,
			},
		},
		"6": {
			ClassType: "VAEEncode",
			Inputs: map[string]any{
				"pixels": []any{"1", 0},
				"vae":    []any{"4", 2},
			},
		},
		"7": {
			ClassType: "VAEDecode",
			Inputs: map[string]any{
				"samples": []any{"5", 0},
				"vae":     []any{"4", 2},
			},
		},
		"8": {
			ClassType: "SaveImage",
			Inputs: map[string]any{
				"images":          []any{"7", 0},
				"filename_prefix": "myagent",
			},
		},
	}

	return &model.ComfyWorkflowInput{
		Prompt:   nodes,
		ClientID: clientID,
	}
}

func resolveCheckpoint(preset string) string {
	if preset != "" {
		return preset
	}
	return "sd_xl_base_1.0.safetensors"
}

func extractTraceCtx(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

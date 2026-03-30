package model

import (
	"context"
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config is the single flat struct loaded by every service via configs.Load().
// Environment variables are mapped via mapstructure tags. Required fields
// cause a panic at startup if missing.
type Config struct {
	ServiceName string `mapstructure:"SERVICE_NAME" required:"true"`
	LogLevel    string `mapstructure:"LOG_LEVEL" default:"info"`
	Port        string `mapstructure:"PORT" default:"8080"`
	GRPCPort    string `mapstructure:"GRPC_PORT" default:"9090"`
	MySQLDSN string `mapstructure:"MYSQL_DSN" required:"true"`

	RedisAddr     string `mapstructure:"REDIS_ADDR" required:"true"`
	RedisPassword string `mapstructure:"REDIS_PASSWORD" default:""`
	RedisDB       int    `mapstructure:"REDIS_DB" default:"0"`

	JWTSecret         string `mapstructure:"JWT_SECRET" required:"true"`
	KafkaBrokers      string `mapstructure:"KAFKA_BROKERS" required:"true"`
	OpenAIKey         string `mapstructure:"OPENAI_API_KEY" required:"true"`
	OrchestratorModel string `mapstructure:"ORCHESTRATOR_MODEL" default:"gpt-4o"`
	PromptAgentModel  string `mapstructure:"PROMPT_AGENT_MODEL" default:"gpt-4o"`
	ComfyUIBaseURL    string `mapstructure:"COMFYUI_BASE_URL" required:"true"`
	AWSBucket         string `mapstructure:"AWS_BUCKET" required:"true"`
	AWSEndpoint       string `mapstructure:"AWS_ENDPOINT" optional:"true"`
	JaegerEndpoint    string `mapstructure:"JAEGER_ENDPOINT" default:"localhost:4317"`
	EncryptionKey     string `mapstructure:"ENCRYPTION_KEY" required:"true"`
}

// ---------------------------------------------------------------------------
// Postgres Models
// ---------------------------------------------------------------------------

// User represents a core user account (table: users). Created on first OAuth
// login and owned by auth-service.
type User struct {
	ID           string    `json:"id" db:"id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	DisplayName  string    `json:"display_name" db:"display_name"`
	AvatarURL    string    `json:"avatar_url" db:"avatar_url"`
	Provider     string    `json:"provider" db:"provider"`
	ProviderID   string    `json:"provider_id" db:"provider_id"`
	Roles        []string  `json:"roles" db:"roles"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// Job represents one full pipeline execution (table: jobs). Owned by the
// orchestrator — tracks the lifecycle from submission through distribution.
type Job struct {
	ID                string          `json:"id" db:"id"`
	UserID            string          `json:"user_id" db:"user_id"`
	Status            string          `json:"status" db:"status"`
	OriginalPrompt    string          `json:"original_prompt" db:"original_prompt"`
	RefinedPrompt     string          `json:"refined_prompt" db:"refined_prompt"`
	OriginalImageURL  string          `json:"original_image_url" db:"original_image_url"`
	GeneratedImageURL string          `json:"generated_image_url" db:"generated_image_url"`
	ExecutionPlan     json.RawMessage `json:"execution_plan" db:"execution_plan"`
	ErrorMessage      string          `json:"error_message" db:"error_message"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

// JobStatusHistory is an immutable, append-only audit log of every status
// transition a job goes through (table: job_status_history).
type JobStatusHistory struct {
	ID         string          `json:"id" db:"id"`
	JobID      string          `json:"job_id" db:"job_id"`
	FromStatus string          `json:"from_status" db:"from_status"`
	ToStatus   string          `json:"to_status" db:"to_status"`
	Service    string          `json:"service" db:"service"`
	Metadata   json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

// PlatformCredential stores per-user OAuth tokens for a connected platform
// (table: platform_credentials). Tokens are encrypted at rest using AES-256-GCM.
type PlatformCredential struct {
	ID              string     `json:"id" db:"id"`
	UserID          string     `json:"user_id" db:"user_id"`
	Platform        string     `json:"platform" db:"platform"`
	AccessTokenEnc  []byte     `json:"-" db:"access_token_enc"`
	RefreshTokenEnc []byte     `json:"-" db:"refresh_token_enc"`
	TokenExpiry     *time.Time `json:"token_expiry" db:"token_expiry"`
	Scopes          []string   `json:"scopes" db:"scopes"`
	PlatformUserID  string     `json:"platform_user_id" db:"platform_user_id"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// PostResult records the outcome of a single posting attempt per platform per
// job (table: post_results). Owned by distribution-service.
type PostResult struct {
	ID             string    `json:"id" db:"id"`
	JobID          string    `json:"job_id" db:"job_id"`
	UserID         string    `json:"user_id" db:"user_id"`
	Platform       string    `json:"platform" db:"platform"`
	Status         string    `json:"status" db:"status"`
	PlatformPostID string    `json:"platform_post_id" db:"platform_post_id"`
	PlatformURL    string    `json:"platform_url" db:"platform_url"`
	ErrorDetail    string    `json:"error_detail" db:"error_detail"`
	AttemptCount   int       `json:"attempt_count" db:"attempt_count"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

// ---------------------------------------------------------------------------
// Job Status Lifecycle
// ---------------------------------------------------------------------------

const (
	JobStatusPending          = "pending"
	JobStatusRefining         = "refining"
	JobStatusGenerating       = "generating"
	JobStatusAwaitingApproval = "awaiting_approval"
	JobStatusApproved         = "approved"
	JobStatusRejected         = "rejected"
	JobStatusDistributing     = "distributing"
	JobStatusDone             = "done"
	JobStatusFailed           = "failed"
)

// ---------------------------------------------------------------------------
// HTTP Request / Response Contracts
// ---------------------------------------------------------------------------

// SubmitJobRequest is the payload for POST /api/v1/jobs (multipart/form-data).
// The image file is handled separately by the handler via c.Request.FormFile.
type SubmitJobRequest struct {
	Prompt    string   `form:"prompt" json:"prompt" validate:"required,max=1000"`
	Platforms []string `form:"platforms" json:"platforms" validate:"required,min=1,dive,oneof=instagram whatsapp discord telegram youtube slack"`
	Caption   string   `form:"caption" json:"caption" validate:"max=2200"`
}

// SubmitJobResponse is returned after a job is accepted into the pipeline.
type SubmitJobResponse struct {
	JobID     string    `json:"job_id"`
	Status    string    `json:"status"`
	WsURL     string    `json:"ws_url"`
	CreatedAt time.Time `json:"created_at"`
}

// WSNotification is a server-to-client WebSocket push message sent at every
// pipeline stage transition.
type WSNotification struct {
	Type       string `json:"type"`
	JobID      string `json:"job_id"`
	Status     string `json:"status"`
	PreviewURL string `json:"preview_url,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ApproveJobRequest is the payload for POST /api/v1/jobs/{job_id}/approve.
type ApproveJobRequest struct {
	Caption   string   `json:"caption,omitempty"`
	Platforms []string `json:"platforms,omitempty"`
}

// RejectJobRequest is the payload for POST /api/v1/jobs/{job_id}/reject.
type RejectJobRequest struct {
	Reason string `json:"reason,omitempty"`
}

// JobActionResponse is the shared response envelope for approve/reject actions.
type JobActionResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// GetJobResponse is the full job detail returned by GET /api/v1/jobs/{job_id}.
type GetJobResponse struct {
	ID                string       `json:"id"`
	Status            string       `json:"status"`
	OriginalPrompt    string       `json:"original_prompt"`
	RefinedPrompt     string       `json:"refined_prompt,omitempty"`
	OriginalImageURL  string       `json:"original_image_url"`
	GeneratedImageURL string       `json:"generated_image_url,omitempty"`
	PostResults       []PostResult `json:"post_results,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
}

// AuthCallbackRequest captures the OAuth2 callback query parameters.
type AuthCallbackRequest struct {
	Code  string `form:"code" validate:"required"`
	State string `form:"state" validate:"required"`
}

// TokenResponse is returned to the client after a successful OAuth exchange.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// ---------------------------------------------------------------------------
// gRPC Contracts (mirror structs — canonical definition in api/proto/auth.proto)
// ---------------------------------------------------------------------------

// ValidateTokenRequest is the gRPC request from api-gateway to auth-service.
type ValidateTokenRequest struct {
	Token string `protobuf:"bytes,1,opt,name=token" json:"token"`
}

// Claims is the gRPC response carrying validated JWT claims.
type Claims struct {
	UserID    string   `protobuf:"bytes,1" json:"user_id"`
	Roles     []string `protobuf:"bytes,2" json:"roles"`
	ExpiresAt int64    `protobuf:"varint,3" json:"expires_at"`
}

// ---------------------------------------------------------------------------
// Kafka Event Models
// ---------------------------------------------------------------------------

// PromptRefinementJob is published to prompt.refine.requested by the
// orchestrator and consumed by the prompt-agent.
type PromptRefinementJob struct {
	JobID          string            `json:"job_id"`
	UserID         string            `json:"user_id"`
	OriginalPrompt string            `json:"original_prompt"`
	ImageURL       string            `json:"image_url"`
	ExecutionPlan  ExecutionPlan     `json:"execution_plan"`
	TraceCtx       map[string]string `json:"trace_ctx"`
	PublishedAt    time.Time         `json:"published_at"`
}

// RefinedPromptEvent is published to prompt.refined by the prompt-agent
// and consumed by the image-gen-agent.
type RefinedPromptEvent struct {
	JobID            string            `json:"job_id"`
	RefinedPrompt    string            `json:"refined_prompt"`
	StyleParams      StyleParams       `json:"style_params"`
	OriginalImageURL string            `json:"original_image_url"`
	TraceCtx         map[string]string `json:"trace_ctx"`
}

// StyleParams holds structured image-generation parameters extracted from
// the LLM's structured output.
type StyleParams struct {
	LightingTemp string  `json:"lighting_temp"`
	AngleDegrees float64 `json:"angle_degrees"`
	DepthOfField string  `json:"depth_of_field"`
	Mood         string  `json:"mood"`
	StylePreset  string  `json:"style_preset"`
}

// ImageGeneratedEvent is published to image.generated by the image-gen-agent
// and consumed by the approval-service.
type ImageGeneratedEvent struct {
	JobID         string            `json:"job_id"`
	UserID        string            `json:"user_id"`
	ImageURL      string            `json:"image_url"`
	ComfyPromptID string            `json:"comfy_prompt_id"`
	GenerationMs  int64             `json:"generation_ms"`
	TraceCtx      map[string]string `json:"trace_ctx"`
}

// ImageApprovedEvent is published to image.approved by the approval-service
// and consumed by the distribution-service.
type ImageApprovedEvent struct {
	JobID     string            `json:"job_id"`
	UserID    string            `json:"user_id"`
	ImageURL  string            `json:"image_url"`
	Caption   string            `json:"caption"`
	Platforms []string          `json:"platforms"`
	TraceCtx  map[string]string `json:"trace_ctx"`
}

// JobFailedEvent is published to job.failed by any agent when an
// unrecoverable error occurs. Consumed by the orchestrator.
type JobFailedEvent struct {
	JobID        string            `json:"job_id"`
	UserID       string            `json:"user_id"`
	FailedAt     string            `json:"failed_at"`
	ErrorMessage string            `json:"error_message"`
	TraceCtx     map[string]string `json:"trace_ctx"`
}

// ---------------------------------------------------------------------------
// Redis Models
// ---------------------------------------------------------------------------

// WSSessionEntry maps a job to the WebSocket node handling the client
// connection. Stored at key ws:session:{job_id} with a 30-minute TTL.
type WSSessionEntry struct {
	NodeID      string    `json:"node_id"`
	UserID      string    `json:"user_id"`
	ConnectedAt time.Time `json:"connected_at"`
}

// OAuthStateEntry is CSRF protection data for OAuth2 flows. Stored at key
// oauth:state:{state} with a 10-minute TTL.
type OAuthStateEntry struct {
	UserAgent string    `json:"user_agent"`
	IP        string    `json:"ip"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

// JobPreviewCache holds the signed S3 URL for a generated image preview.
// Stored at key job:preview:{job_id} with a 1-hour TTL.
type JobPreviewCache struct {
	SignedURL string    `json:"signed_url"`
	ImageURL  string    `json:"image_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ---------------------------------------------------------------------------
// Domain Models
// ---------------------------------------------------------------------------

// ExecutionPlan is the structured output of the Orchestrator LLM call,
// representing parsed user intent for the editing pipeline.
type ExecutionPlan struct {
	Edits             []EditInstruction `json:"edits"`
	Style             StyleParams       `json:"style"`
	BackgroundReplace bool              `json:"background_replace"`
	SubjectPreserve   bool              `json:"subject_preserve"`
	Mood              string            `json:"mood"`
}

// EditInstruction describes a single editing operation within an ExecutionPlan.
type EditInstruction struct {
	Operation   string `json:"operation"`
	Target      string `json:"target"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
}

// PostRequest is the platform-agnostic input for PlatformConnector.Publish().
type PostRequest struct {
	MediaURL string            `json:"media_url"`
	Caption  string            `json:"caption"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// PlatformConnector defines the interface every social-platform connector
// must implement.
type PlatformConnector interface {
	Publish(ctx context.Context, req PostRequest) error
	Validate(ctx context.Context) error
	Name() string
}

// ComfyWorkflowInput is the rendered workflow JSON sent to ComfyUI's
// /prompt endpoint.
type ComfyWorkflowInput struct {
	Prompt   map[string]ComfyNode `json:"prompt"`
	ClientID string               `json:"client_id"`
}

// ComfyNode represents a single node in the ComfyUI workflow graph.
type ComfyNode struct {
	ClassType string         `json:"class_type"`
	Inputs    map[string]any `json:"inputs"`
}

// AuthenticatedUser is injected into the Gin context by the JWT middleware
// after token validation. Every downstream handler receives this.
type AuthenticatedUser struct {
	UserID string
	Roles  []string
	Email  string
}

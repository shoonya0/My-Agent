package types

import (
	"encoding/json"
	"time"
)

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
	Platforms         []string        `json:"platforms,omitempty" db:"platforms"`
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
	ID              string            `json:"id" db:"id"`
	UserID          string            `json:"user_id" db:"user_id"`
	Platform        string            `json:"platform" db:"platform"`
	AccessTokenEnc  []byte            `json:"-" db:"access_token_enc"`
	RefreshTokenEnc []byte            `json:"-" db:"refresh_token_enc"`
	TokenExpiry     *time.Time        `json:"token_expiry" db:"token_expiry"`
	Scopes          []string          `json:"scopes" db:"scopes"`
	PlatformUserID  string            `json:"platform_user_id" db:"platform_user_id"`
	Metadata        map[string]string `json:"metadata" db:"metadata"`
	CreatedAt       time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at" db:"updated_at"`
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

// StyleParams holds structured image-generation parameters extracted from
// the LLM's structured output.
type StyleParams struct {
	LightingTemp string  `json:"lighting_temp"`
	AngleDegrees float64 `json:"angle_degrees"`
	DepthOfField string  `json:"depth_of_field"`
	Mood         string  `json:"mood"`
	StylePreset  string  `json:"style_preset"`
}

// PostRequest is the platform-agnostic input for PlatformConnector.Publish().
type PostRequest struct {
	MediaURL string            `json:"media_url"`
	Caption  string            `json:"caption"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// PublishResult carries platform-specific identifiers returned by a
// successful Publish call, used to populate PostResult records.
type PublishResult struct {
	PlatformPostID string `json:"platform_post_id"`
	PlatformURL    string `json:"platform_url"`
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

// WSSessionEntry maps a job to the WebSocket node handling the client
// connection. Stored at key ws:session:{job_id} with a 30-minute TTL.
type WSSessionEntry struct {
	NodeID      string    `json:"node_id"`
	UserID      string    `json:"user_id"`
	ConnectedAt time.Time `json:"connected_at"`
}

// JobPreviewCache holds the signed S3 URL for a generated image preview.
// Stored at key job:preview:{job_id} with a 1-hour TTL.
type JobPreviewCache struct {
	SignedURL string    `json:"signed_url"`
	ImageURL  string    `json:"image_url"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

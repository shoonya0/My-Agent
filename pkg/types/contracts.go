package types

import "time"

// SubmitJobRequest is the payload for POST /api/v1/jobs (multipart/form-data).
// The image file is handled separately by the handler via c.Request.FormFile.
type SubmitJobRequest struct {
	Prompt    string   `form:"prompt" json:"prompt" validate:"required,max=1000"`
	Platforms []string `form:"platforms" json:"platforms" validate:"required,min=1,dive,oneof=instagram whatsapp discord telegram"`
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
	Platforms         []string     `json:"platforms,omitempty"`
	PostResults       []PostResult `json:"post_results,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
}

// ConnectPlatformRequest is the payload for POST /api/v1/credentials.
type ConnectPlatformRequest struct {
	Platform string            `json:"platform" binding:"required,oneof=instagram whatsapp discord telegram"`
	Token    string            `json:"token" binding:"required"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdatePlatformRequest is the payload for PUT /api/v1/credentials/:platform.
type UpdatePlatformRequest struct {
	Token    string            `json:"token" binding:"required"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// PlatformCredentialResponse is the safe (token-masked) view returned to clients.
type PlatformCredentialResponse struct {
	Platform       string            `json:"platform"`
	Connected      bool              `json:"connected"`
	PlatformUserID string            `json:"platform_user_id,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	ConnectedAt    time.Time         `json:"connected_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// ListCredentialsResponse wraps a slice of connected platforms.
type ListCredentialsResponse struct {
	Credentials []PlatformCredentialResponse `json:"credentials"`
}

// TokenResponse is returned to the client after a successful OAuth exchange.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// Claims is the gRPC response carrying validated JWT claims (used by auth-service and middleware).
type Claims struct {
	UserID    string   `protobuf:"bytes,1" json:"user_id"`
	Roles     []string `protobuf:"bytes,2" json:"roles"`
	ExpiresAt int64    `protobuf:"varint,3" json:"expires_at"`
}

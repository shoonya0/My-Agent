package model

import "time"

// field	type	description
// ServiceNamereq	string	Required. Used in OTel resource and log fields.
// LogLevelreq	string	Default: info. Options: debug | info | warn | error
// Portreq	string	Default: 8080. HTTP listener port.
// GRPCPortreq	string	Default: 9090. gRPC listener port (auth-service).
// PostgresDSNreq	string	Required by auth-service, orchestrator. Postgres connection string.
// RedisAddrreq	string	Required. Redis host:port.
// JWTSecretreq	string	Required. Minimum 32 chars. Used to sign and verify JWTs.
// KafkaBrokersreq	string	Required. Comma-separated list of broker addresses.
// OpenAIKeyreq	string	Required by orchestrator and prompt-agent.
// OrchestratorModelreq	string	Default: gpt-4o. LLM model for intent parsing.
// PromptAgentModelreq	string	Default: gpt-4o. Model for prompt refinement.
// ComfyUIBaseURLreq	string	Required by image-gen-agent.
// AWSBucketreq	string	Required. S3/MinIO bucket name.
// AWSEndpointopt	string	Optional. Set for MinIO local dev; blank for real AWS.
// JaegerEndpointreq	string	Default: localhost:4317. OTLP gRPC endpoint.
// EncryptionKeyreq	string	Required. 32-byte AES key for encrypting platform tokens at rest.
// type Config struct { ServiceName string "st">`envconfig:"SERVICE_NAME" required:"true"` LogLevel string "st">`envconfig:"LOG_LEVEL" default:"info"` Port string "st">`envconfig:"PORT" default:"8080"` GRPCPort string "st">`envconfig:"GRPC_PORT" default:"9090"` PostgresDSN string "st">`envconfig:"POSTGRES_DSN" required:"true"` RedisAddr string "st">`envconfig:"REDIS_ADDR" required:"true"` JWTSecret string "st">`envconfig:"JWT_SECRET" required:"true"` KafkaBrokers string "st">`envconfig:"KAFKA_BROKERS" required:"true"` OpenAIKey string "st">`envconfig:"OPENAI_API_KEY" required:"true"`

type Config struct {
	ServiceName string `mapstructure:"SERVICE_NAME" required:"true"`
	LogLevel    string `mapstructure:"LOG_LEVEL" default:"info"`
	Port        string `mapstructure:"PORT" default:"8080"`
	GRPCPort    string `mapstructure:"GRPC_PORT" default:"9090"`
	PostgresDSN string `mapstructure:"POSTGRES_DSN" required:"true"`

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

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url"`
	Provider    string    `json:"provider"`
	ProviderID  string    `json:"provider_id"`
	Roles       []string  `json:"roles"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Job struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	Status            string    `json:"status"`
	OriginalPrompt    string    `json:"original_prompt"`
	RefinedPrompt     string    `json:"refined_prompt"`
	OriginalImageURL  string    `json:"original_image_url"`
	GeneratedImageURL string    `json:"generated_image_url"`
	ExecutionPlan     string    `json:"execution_plan"`
	ErrorMessage      string    `json:"error_message"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type JobStatusHistory struct {
	ID         string    `json:"id"`
	JobID      string    `json:"job_id"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Service    string    `json:"service"`
	Metadata   string    `json:"metadata"`
	CreatedAt  time.Time `json:"created_at"`
}

type PlatformCredentials struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Platform       string    `json:"platform"`
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	TokenExpiry    time.Time `json:"token_expiry"`
	Scopes         []string  `json:"scopes"`
	PlatformUserID string    `json:"platform_user_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PostResults struct {
	ID             string    `json:"id"`
	JobID          string    `json:"job_id"`
	UserID         string    `json:"user_id"`
	Platform       string    `json:"platform"`
	Status         string    `json:"status"`
	PlatformPostID string    `json:"platform_post_id"`
	PlatformURL    string    `json:"platform_url"`
	ErrorDetail    string    `json:"error_detail"`
	AttemptCount   int       `json:"attempt_count"`
	CreatedAt      time.Time `json:"created_at"`
}

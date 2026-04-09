package types

// Config is the single flat struct loaded by every service via configs.Load().
// Environment variables are mapped via mapstructure tags. Required fields
// cause a panic at startup if missing.
type Config struct {
	LogLevel string `mapstructure:"LOG_LEVEL" default:"info"`
	GRPCPort string `mapstructure:"GRPC_PORT" default:"9090"`
	MySQLDSN string `mapstructure:"MYSQL_DSN" required:"true"`

	// Service Names and Ports
	APIServiceName           string `mapstructure:"API_SERVICE_NAME" default:"api-gateway"`
	APIGatewayPort           string `mapstructure:"API_GATEWAY_PORT" default:"8080"`
	AuthServiceName          string `mapstructure:"AUTH_SERVICE_NAME" default:"auth-service"`
	AuthServicePort          string `mapstructure:"AUTH_SERVICE_PORT" default:"8082"`
	OrchestratorServiceName  string `mapstructure:"ORCHESTRATOR_SERVICE_NAME" default:"orchestrator"`
	OrchestratorPort         string `mapstructure:"ORCHESTRATOR_PORT" default:"8083"`
	PromptAgentServiceName   string `mapstructure:"PROMPT_AGENT_SERVICE_NAME" default:"prompt-agent"`
	PromptAgentPort          string `mapstructure:"PROMPT_AGENT_PORT" default:"8084"`
	ImageGenAgentServiceName string `mapstructure:"IMAGE_GEN_AGENT_SERVICE_NAME" default:"image-gen-agent"`
	ImageGenAgentPort        string `mapstructure:"IMAGE_GEN_AGENT_PORT" default:"8085"`
	ApprovalServiceName      string `mapstructure:"APPROVAL_SERVICE_NAME" default:"approval-service"`
	ApprovalServicePort      string `mapstructure:"APPROVAL_SERVICE_PORT" default:"8086"`
	DistributionServiceName  string `mapstructure:"DISTRIBUTION_SERVICE_NAME" default:"distribution-service"`
	DistributionPort         string `mapstructure:"DISTRIBUTION_PORT" default:"8087"`

	RedisAddr     string `mapstructure:"REDIS_ADDR" required:"true"`
	RedisPassword string `mapstructure:"REDIS_PASSWORD" default:""`
	RedisDB       int    `mapstructure:"REDIS_DB" default:"0"`

	JWTSecret               string `mapstructure:"JWT_SECRET" required:"true"`
	KafkaBrokers            string `mapstructure:"KAFKA_BROKERS" required:"true"`
	OpenAIKey               string `mapstructure:"OPENAI_API_KEY" required:"true"`
	OrchestratorModel       string `mapstructure:"ORCHESTRATOR_MODEL" default:"gpt-4o"`
	PromptAgentModel        string `mapstructure:"PROMPT_AGENT_MODEL" default:"gpt-4o"`
	ComfyUIBaseURL          string `mapstructure:"COMFYUI_BASE_URL" required:"true"`
	AWSBucket               string `mapstructure:"AWS_BUCKET" required:"true"`
	AWSEndpoint             string `mapstructure:"AWS_ENDPOINT" optional:"true"`
	AWSRegion               string `mapstructure:"AWS_REGION" default:"us-east-1"`
	AWSAccessKeyID          string `mapstructure:"AWS_ACCESS_KEY_ID" optional:"true"`
	AWSSecretAccessKey      string `mapstructure:"AWS_SECRET_ACCESS_KEY" optional:"true"`
	AuthServiceAddr         string `mapstructure:"AUTH_SERVICE_ADDR" default:"localhost:9090"`
	OrchestratorServiceAddr string `mapstructure:"ORCHESTRATOR_SERVICE_ADDR" default:"localhost:9091"`
	OrchestratorGRPCPort    string `mapstructure:"ORCHESTRATOR_GRPC_PORT" default:"9091"`
	ApprovalServiceAddr     string `mapstructure:"APPROVAL_SERVICE_ADDR" default:"localhost:9092"`
	ApprovalGRPCPort        string `mapstructure:"APPROVAL_GRPC_PORT" default:"9092"`
	JaegerEndpoint          string `mapstructure:"JAEGER_ENDPOINT" default:"localhost:4317"`
	EncryptionKey           string `mapstructure:"ENCRYPTION_KEY" required:"true"`
	PromptAgentSystemPrompt string `mapstructure:"PROMPT_AGENT_SYSTEM_PROMPT" optional:"true"`

	// OAuth (optional; required only for HandleOAuthCallback on supported providers)
	GoogleOAuthClientID     string `mapstructure:"GOOGLE_OAUTH_CLIENT_ID" optional:"true"`
	GoogleOAuthClientSecret string `mapstructure:"GOOGLE_OAUTH_CLIENT_SECRET" optional:"true"`
	GoogleOAuthRedirectURL  string `mapstructure:"GOOGLE_OAUTH_REDIRECT_URL" optional:"true"`
	GithubOAuthClientID     string `mapstructure:"GITHUB_OAUTH_CLIENT_ID" optional:"true"`
	GithubOAuthClientSecret string `mapstructure:"GITHUB_OAUTH_CLIENT_SECRET" optional:"true"`
	GithubOAuthRedirectURL  string `mapstructure:"GITHUB_OAUTH_REDIRECT_URL" optional:"true"`
}

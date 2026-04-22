package config

import (
	"log"
	"myAgent/pkg/types"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Load reads configuration from a file and/or environment variables and
// returns a validated *types.Config. Set CONFIG_PATH to override the file
// search. Environment variables always take precedence over file values.
func Load() *types.Config {
	cfg := &types.Config{}

	if path := os.Getenv("CONFIG_PATH"); path != "" {
		viper.SetConfigFile(path)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("env")
		viper.AddConfigPath(".")
		viper.AddConfigPath("../..")
	}

	viper.AutomaticEnv()

	// Viper does not apply struct `default` tags. Without these, omitted keys
	// unmarshal to "" and gRPC can listen on ":0" (wrong) while clients dial
	// localhost:9093, etc. Env and config file still override.
	viper.SetDefault("ORCHESTRATOR_GRPC_PORT", "9091")
	viper.SetDefault("APPROVAL_GRPC_PORT", "9093")
	viper.SetDefault("AUTH_SERVICE_ADDR", "localhost:9190")
	viper.SetDefault("ORCHESTRATOR_SERVICE_ADDR", "localhost:9091")
	viper.SetDefault("APPROVAL_SERVICE_ADDR", "localhost:9093")

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Config file not found, relying on environment variables: %v", err)
	}

	if err := viper.Unmarshal(cfg); err != nil {
		log.Fatalf("Error unmarshalling config: %v", err)
	}

	validateRequired(cfg)

	return cfg
}

// validateRequired panics at startup with a clear message when any required
// config value is missing — fail-fast rather than silent runtime errors.
func validateRequired(cfg *types.Config) {
	required := map[string]string{
		"MYSQL_DSN":        cfg.MySQLDSN,
		"REDIS_ADDR":       cfg.RedisAddr,
		"JWT_SECRET":       cfg.JWTSecret,
		"KAFKA_BROKERS":    cfg.KafkaBrokers,
		"OPENAI_API_KEY":   cfg.OpenAIKey,
		"COMFYUI_BASE_URL": cfg.ComfyUIBaseURL,
		"AWS_BUCKET":       cfg.AWSBucket,
		"ENCRYPTION_KEY":   cfg.EncryptionKey,
	}

	var missing []string
	for env, val := range required {
		if val == "" {
			missing = append(missing, env)
		}
	}

	if len(missing) > 0 {
		log.Fatalf("Missing required config values: %s", strings.Join(missing, ", "))
	}
}

package config

import (
	"log"
	"myAgent/pkg/model"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Load reads configuration from a file and/or environment variables and
// returns a validated *model.Config. Set CONFIG_PATH to override the file
// search. Environment variables always take precedence over file values.
func Load() *model.Config {
	cfg := &model.Config{}

	if path := os.Getenv("CONFIG_PATH"); path != "" {
		viper.SetConfigFile(path)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("env")
		viper.AddConfigPath(".")
		viper.AddConfigPath("../..")
	}

	viper.AutomaticEnv()

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
func validateRequired(cfg *model.Config) {
	required := map[string]string{
		"SERVICE_NAME": cfg.ServiceName,
		"REDIS_ADDR":   cfg.RedisAddr,
		"JWT_SECRET":   cfg.JWTSecret,
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

package config

import (
	"log"
	"myAgent/pkg/model"

	"github.com/spf13/viper"
)

func Load() model.Config {
	cfg := model.Config{}

	viper.SetConfigFile("../../config.env")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config: %v", err)
	}

	if err := viper.Unmarshal(&cfg); err != nil {
		log.Fatalf("Error unmarshalling config: %v", err)
	}

	return cfg
}

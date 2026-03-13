package config

import (
	"log"
	"os"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env string `yaml:"env" env-default:"local"`

	// path to the SQLite database file
	StoragePath string `yaml:"storage_path" env-required:"true"`

	// telegram bot token
	TelegramToken string  `yaml:"telegram_token" env-required:"true"`

	// values used to build VPN keys
	ServerIP string `yaml:"server_ip" env-required:"true"`
	PublicKey string `yaml:"public_key" env-required:"true"`
	ShortID string `yaml:"short_id" env-required:"true"`
	SNI string `yaml:"sni" env-required:"true"`

	// path to the xray JSON configuration file; during development this
	// file may not actually exist, but the field is required for
	// dependency injection.
	XrayConfigPath string `yaml:"xray_config_path" env-required:"true"`

	// interval at which the background expiration checker runs. Accepts
	// any duration string supported by time.ParseDuration (e.g. "5m").
	ExpirationInterval string `yaml:"expiration_interval" env-default:"5m"`
}

func MustLoad() *Config {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		log.Fatal("CONFIG_PATH is not set")
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatalf("Config file does not exist: %s", configPath)
	}

	var cfg Config

	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	return &cfg
}
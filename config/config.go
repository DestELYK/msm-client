package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/joho/godotenv"
)

type ClientConfig struct {
	ClientID        string `json:"client_id"`
	DeviceName      string `json:"device_name,omitempty"`      // Optional device name
	UpdateInterval  int    `json:"update_interval,omitempty"`  // in seconds
	DisableCommands bool   `json:"disable_commands,omitempty"` // Disable remote command execution
}

const DEFAULT_PATH = "/etc/msm-client" // Default path for config file

const configFile = "client.json"

// getConfigPath returns the path for the config file based on environment variable or default
func getConfigPath() string {
	if path := os.Getenv("MSC_CONFIG_PATH"); path != "" {
		return filepath.Join(path, configFile)
	}
	return filepath.Join(DEFAULT_PATH, configFile)
}

func LoadOrCreateConfig() (ClientConfig, error) {
	var cfg ClientConfig
	configPath := getConfigPath()

	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
		if err := ValidateConfig(cfg); err != nil {
			return cfg, err
		}
		return cfg, nil
	}

	// Generate new UUID
	cfg.ClientID = uuid.New().String()
	cfg.UpdateInterval = 30 // Set default update interval
	if err := SaveConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func ValidateConfig(cfg ClientConfig) error {
	if cfg.ClientID == "" {
		return errors.New("missing client_id")
	}
	if cfg.UpdateInterval <= 0 {
		return errors.New("update_interval must be greater than 0")
	}
	// Validate UUID format
	_, err := uuid.Parse(cfg.ClientID)
	return err
}

func SaveConfig(cfg ClientConfig) error {
	configPath := getConfigPath()

	// Create directory if it doesn't exist
	if dir := filepath.Dir(configPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0600)
}

func LoadEnv() error {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		fmt.Println("Failed to load .env file, using system environment variables")
	}

	if os.Getenv("MSM_SECRET_KEY") == "" {
		return errors.New("MSM_SECRET_KEY environment variable is required")
	}
	return nil
}

func GetSecretKey() string {
	secretKey := os.Getenv("MSM_SECRET_KEY")
	if secretKey == "" {
		panic("MSM_SECRET_KEY environment variable is not set")
	}
	return secretKey
}

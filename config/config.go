package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/joho/godotenv"
)

type ClientConfig struct {
	ClientID       string `json:"client_id"`
	DeviceName     string `json:"device_name,omitempty"`     // Optional device name
	UpdateInterval int    `json:"update_interval,omitempty"` // in seconds
}

const configFile = "client.json"

func LoadOrCreateConfig() (ClientConfig, error) {
	var cfg ClientConfig

	if data, err := os.ReadFile(configFile); err == nil {
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
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0600)
}

func LoadEnv() error {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		fmt.Println("Failed to load .env file, using system environment variables")
	}

	if err := os.Getenv("MSM_SECRET_KEY"); err == "" {
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

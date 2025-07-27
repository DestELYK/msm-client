package config

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestLoadOrCreateConfig(t *testing.T) {
	// Clean up any existing config file
	defer os.Remove(configFile)

	// Test creating new config
	cfg, err := LoadOrCreateConfig()
	if err != nil {
		t.Fatalf("Failed to create new config: %v", err)
	}

	// Verify generated UUID is valid
	_, err = uuid.Parse(cfg.ClientID)
	if err != nil {
		t.Fatalf("Generated ClientID is not a valid UUID: %v", err)
	}

	// Verify config file was created
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	// Test loading existing config
	cfg2, err := LoadOrCreateConfig()
	if err != nil {
		t.Fatalf("Failed to load existing config: %v", err)
	}

	if cfg.ClientID != cfg2.ClientID {
		t.Fatalf("Expected ClientID to be %s, got %s", cfg.ClientID, cfg2.ClientID)
	}
}

func TestSaveConfig(t *testing.T) {
	defer os.Remove(configFile)

	testConfig := ClientConfig{
		ClientID:       "test-client-id",
		DeviceName:     "Test Device",
		UpdateInterval: 30,
	}

	err := SaveConfig(testConfig)
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Verify file exists and content is correct
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	var savedConfig ClientConfig
	err = json.Unmarshal(data, &savedConfig)
	if err != nil {
		t.Fatalf("Failed to unmarshal saved config: %v", err)
	}

	if savedConfig.ClientID != testConfig.ClientID {
		t.Fatalf("Expected ClientID %s, got %s", testConfig.ClientID, savedConfig.ClientID)
	}
	if savedConfig.DeviceName != testConfig.DeviceName {
		t.Fatalf("Expected DeviceName %s, got %s", testConfig.DeviceName, savedConfig.DeviceName)
	}
	if savedConfig.UpdateInterval != testConfig.UpdateInterval {
		t.Fatalf("Expected UpdateInterval %d, got %d", testConfig.UpdateInterval, savedConfig.UpdateInterval)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      ClientConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: ClientConfig{
				ClientID:       uuid.New().String(),
				DeviceName:     "Test Device",
				UpdateInterval: 30,
			},
			expectError: false,
		},
		{
			name: "missing client_id",
			config: ClientConfig{
				DeviceName:     "Test Device",
				UpdateInterval: 30,
			},
			expectError: true,
			errorMsg:    "missing client_id",
		},
		{
			name: "zero update_interval",
			config: ClientConfig{
				ClientID:       uuid.New().String(),
				DeviceName:     "Test Device",
				UpdateInterval: 0,
			},
			expectError: true,
			errorMsg:    "update_interval must be greater than 0",
		},
		{
			name: "negative update_interval",
			config: ClientConfig{
				ClientID:       uuid.New().String(),
				DeviceName:     "Test Device",
				UpdateInterval: -1,
			},
			expectError: true,
			errorMsg:    "update_interval must be greater than 0",
		},
		{
			name: "invalid UUID format",
			config: ClientConfig{
				ClientID:       "not-a-valid-uuid",
				DeviceName:     "Test Device",
				UpdateInterval: 30,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error but got none")
				}
				if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Fatalf("Expected error message '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestLoadEnv(t *testing.T) {
	// Set up environment variable first
	os.Setenv("MSM_SECRET_KEY", "test-secret-key")
	defer os.Unsetenv("MSM_SECRET_KEY")

	// Create a temporary .env file
	envContent := `TEST_VAR=test_value
CLIENT_NAME=test_client
`
	err := os.WriteFile(".env", []byte(envContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test .env file: %v", err)
	}
	defer os.Remove(".env")

	err = LoadEnv()
	if err != nil {
		t.Fatalf("Failed to load .env file: %v", err)
	}

	// Verify environment variables were loaded
	if os.Getenv("TEST_VAR") != "test_value" {
		t.Fatalf("Expected TEST_VAR to be 'test_value', got '%s'", os.Getenv("TEST_VAR"))
	}
	if os.Getenv("CLIENT_NAME") != "test_client" {
		t.Fatalf("Expected CLIENT_NAME to be 'test_client', got '%s'", os.Getenv("CLIENT_NAME"))
	}
}

func TestLoadEnvFileNotExists(t *testing.T) {
	// Set up environment variable first
	os.Setenv("MSM_SECRET_KEY", "test-secret-key")
	defer os.Unsetenv("MSM_SECRET_KEY")

	// Ensure no .env file exists
	os.Remove(".env")

	err := LoadEnv()
	// Should not error when .env file doesn't exist if MSM_SECRET_KEY is set
	if err != nil {
		t.Fatalf("LoadEnv should not error when .env file doesn't exist but MSM_SECRET_KEY is set: %v", err)
	}
}

func TestLoadConfigWithCorruptedFile(t *testing.T) {
	defer os.Remove(configFile)

	// Create a corrupted JSON file
	err := os.WriteFile(configFile, []byte("invalid json content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create corrupted config file: %v", err)
	}

	_, err = LoadOrCreateConfig()
	if err == nil {
		t.Fatal("Expected error when loading corrupted config file")
	}
}

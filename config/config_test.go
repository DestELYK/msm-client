package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIPValidationModes(t *testing.T) {
	var cfg ClientConfig

	// Test strict mode
	cfg.SetStrictIPValidation()
	if !cfg.StrictIPValidation || cfg.AllowIPSubnetMatch || cfg.DisableIPValidation {
		t.Error("SetStrictIPValidation() did not set correct flags")
	}
	if cfg.GetIPValidationMode() != "strict" {
		t.Errorf("Expected 'strict', got '%s'", cfg.GetIPValidationMode())
	}

	// Test subnet mode
	cfg.SetSubnetIPValidation()
	if cfg.StrictIPValidation || !cfg.AllowIPSubnetMatch || cfg.DisableIPValidation {
		t.Error("SetSubnetIPValidation() did not set correct flags")
	}
	if cfg.GetIPValidationMode() != "subnet" {
		t.Errorf("Expected 'subnet', got '%s'", cfg.GetIPValidationMode())
	}

	// Test permissive mode
	cfg.SetPermissiveIPValidation()
	if cfg.StrictIPValidation || cfg.AllowIPSubnetMatch || cfg.DisableIPValidation {
		t.Error("SetPermissiveIPValidation() did not set correct flags")
	}
	if cfg.GetIPValidationMode() != "permissive" {
		t.Errorf("Expected 'permissive', got '%s'", cfg.GetIPValidationMode())
	}

	// Test disabled mode
	cfg.DisableAllIPValidation()
	if cfg.StrictIPValidation || cfg.AllowIPSubnetMatch || !cfg.DisableIPValidation {
		t.Error("DisableAllIPValidation() did not set correct flags")
	}
	if cfg.GetIPValidationMode() != "disabled" {
		t.Errorf("Expected 'disabled', got '%s'", cfg.GetIPValidationMode())
	}
}

func TestDefaultConfig(t *testing.T) {
	// Test that new configs have sensible defaults
	cfg := ClientConfig{
		ClientID:             "test-client",
		StatusUpdateInterval: 30 * time.Second,
		StrictIPValidation:   false,
		AllowIPSubnetMatch:   true, // This should be the default
		DisableIPValidation:  false,
	}

	mode := cfg.GetIPValidationMode()
	if mode != "subnet" {
		t.Errorf("Default config should use subnet validation, got '%s'", mode)
	}

	// Test status update interval getter
	interval := cfg.GetStatusUpdateInterval()
	if interval != 30*time.Second {
		t.Errorf("Expected status update interval of 30s, got %v", interval)
	}
}

func TestSecuritySettings(t *testing.T) {
	var cfg ClientConfig

	// Test default values for security settings
	maxViolations := cfg.GetMaxIPViolations()
	if maxViolations != 3 {
		t.Errorf("Expected default max violations of 3, got %d", maxViolations)
	}

	blacklistDuration := cfg.GetIPBlacklistDuration()
	if blacklistDuration != 1*time.Hour {
		t.Errorf("Expected default blacklist duration of 1 hour, got %v", blacklistDuration)
	}

	// Test default values for verification code settings
	codeLength := cfg.GetVerificationCodeLength()
	if codeLength != 6 {
		t.Errorf("Expected default code length of 6, got %d", codeLength)
	}

	codeAttempts := cfg.GetVerificationCodeAttempts()
	if codeAttempts != 3 {
		t.Errorf("Expected default code attempts of 3, got %d", codeAttempts)
	}

	codeExpiration := cfg.GetPairingCodeExpiration()
	if codeExpiration != 2*time.Minute {
		t.Errorf("Expected default code expiration of 2 minutes, got %v", codeExpiration)
	}

	// Test explicit values
	cfg.MaxIPViolations = 5
	cfg.IPBlacklistDuration = 2 * time.Hour
	cfg.VerificationCodeLength = 8
	cfg.VerificationCodeAttempts = 5
	cfg.PairingCodeExpiration = 2 * time.Minute

	if cfg.GetMaxIPViolations() != 5 {
		t.Errorf("Expected max violations of 5, got %d", cfg.GetMaxIPViolations())
	}

	if cfg.GetIPBlacklistDuration() != 2*time.Hour {
		t.Errorf("Expected blacklist duration of 2 hours, got %v", cfg.GetIPBlacklistDuration())
	}

	if cfg.GetVerificationCodeLength() != 8 {
		t.Errorf("Expected code length of 8, got %d", cfg.GetVerificationCodeLength())
	}

	if cfg.GetVerificationCodeAttempts() != 5 {
		t.Errorf("Expected code attempts of 5, got %d", cfg.GetVerificationCodeAttempts())
	}

	if cfg.GetPairingCodeExpiration() != 2*time.Minute {
		t.Errorf("Expected code expiration of 2 minutes, got %v", cfg.GetPairingCodeExpiration())
	}

	// Test zero/negative values fall back to defaults
	cfg.MaxIPViolations = 0
	cfg.IPBlacklistDuration = -1 * time.Hour
	cfg.VerificationCodeLength = 0
	cfg.VerificationCodeAttempts = -1

	if cfg.GetMaxIPViolations() != 3 {
		t.Errorf("Expected fallback to default max violations of 3, got %d", cfg.GetMaxIPViolations())
	}

	if cfg.GetIPBlacklistDuration() != 1*time.Hour {
		t.Errorf("Expected fallback to default blacklist duration of 1 hour, got %v", cfg.GetIPBlacklistDuration())
	}

	if cfg.GetVerificationCodeLength() != 6 {
		t.Errorf("Expected fallback to default code length of 6, got %d", cfg.GetVerificationCodeLength())
	}

	if cfg.GetVerificationCodeAttempts() != 3 {
		t.Errorf("Expected fallback to default code attempts of 3, got %d", cfg.GetVerificationCodeAttempts())
	}
}

func TestConfigValidation(t *testing.T) {
	// Test that validation passes after auto-correction of invalid values
	cfg := ClientConfig{
		ClientID:                 "550e8400-e29b-41d4-a716-446655440000", // Valid UUID
		StatusUpdateInterval:     30 * time.Second,
		MaxIPViolations:          -1,
		IPBlacklistDuration:      -1 * time.Hour,
		VerificationCodeLength:   -1,
		VerificationCodeAttempts: -1,
	}

	// Now validation should pass because invalid values are auto-corrected before validation
	_, err := ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected validation to pass after auto-correction, got: %v", err)
	}

	// Test validation with valid values
	cfg.MaxIPViolations = 5
	cfg.IPBlacklistDuration = 2 * time.Hour
	cfg.VerificationCodeLength = 8
	cfg.VerificationCodeAttempts = 5

	_, err = ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected no validation error, got: %v", err)
	}

	// Test validation with invalid status update interval - should pass after auto-correction
	cfg.StatusUpdateInterval = 0
	_, err = ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected validation to pass after auto-correction, got: %v", err)
	}

	// Test validation with negative status update interval - should pass after auto-correction
	cfg.StatusUpdateInterval = -1 * time.Second
	_, err = ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected validation to pass after auto-correction, got: %v", err)
	}
}

func TestAutoCorrection(t *testing.T) {
	// Test auto-correction behavior using ValidateConfig

	// Test StatusUpdateInterval correction
	cfg := ClientConfig{
		ClientID:             "550e8400-e29b-41d4-a716-446655440000",
		StatusUpdateInterval: -1 * time.Second, // Invalid
	}

	correctedCfg, err := ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected validation to succeed with auto-correction, got: %v", err)
	}

	if correctedCfg.StatusUpdateInterval != 5*time.Second {
		t.Errorf("Expected auto-corrected StatusUpdateInterval to be 5s, got %v", correctedCfg.StatusUpdateInterval)
	}

	// Test MaxIPViolations correction
	cfg.MaxIPViolations = -5 // Invalid
	correctedCfg, err = ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected validation to succeed with auto-correction, got: %v", err)
	}

	if correctedCfg.MaxIPViolations != 3 {
		t.Errorf("Expected auto-corrected MaxIPViolations to be 3, got %d", correctedCfg.MaxIPViolations)
	}

	// Test VerificationCodeLength correction
	cfg.VerificationCodeLength = -1 // Invalid
	correctedCfg, err = ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected validation to succeed with auto-correction, got: %v", err)
	}

	if correctedCfg.VerificationCodeLength != 6 {
		t.Errorf("Expected auto-corrected VerificationCodeLength to be 6, got %d", correctedCfg.VerificationCodeLength)
	}

	// Test VerificationCodeAttempts correction
	cfg.VerificationCodeAttempts = 0 // Invalid
	correctedCfg, err = ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected validation to succeed with auto-correction, got: %v", err)
	}

	if correctedCfg.VerificationCodeAttempts != 3 {
		t.Errorf("Expected auto-corrected VerificationCodeAttempts to be 3, got %d", correctedCfg.VerificationCodeAttempts)
	}
}

func TestStatusUpdateInterval(t *testing.T) {
	var cfg ClientConfig

	// Test default value
	interval := cfg.GetStatusUpdateInterval()
	if interval != 5*time.Second {
		t.Errorf("Expected default status update interval of 5s, got %v", interval)
	}

	// Test explicit value
	cfg.StatusUpdateInterval = 10 * time.Second
	interval = cfg.GetStatusUpdateInterval()
	if interval != 10*time.Second {
		t.Errorf("Expected status update interval of 10s, got %v", interval)
	}

	// Test zero value falls back to default
	cfg.StatusUpdateInterval = 0
	interval = cfg.GetStatusUpdateInterval()
	if interval != 5*time.Second {
		t.Errorf("Expected fallback to default status update interval of 5s, got %v", interval)
	}

	// Test negative value falls back to default
	cfg.StatusUpdateInterval = -1 * time.Second
	interval = cfg.GetStatusUpdateInterval()
	if interval != 5*time.Second {
		t.Errorf("Expected fallback to default status update interval of 5s, got %v", interval)
	}
}

func TestScreenSwitchPath(t *testing.T) {
	var cfg ClientConfig

	// Test default value
	path := cfg.GetScreenSwitchPath()
	expectedDefault := "/usr/local/bin/mediascreen-installer/scripts/screen-switch.sh"
	if path != expectedDefault {
		t.Errorf("Expected default screen switch path '%s', got '%s'", expectedDefault, path)
	}

	// Test explicit value
	cfg.ScreenSwitchPath = "/custom/path/to/script.sh"
	path = cfg.GetScreenSwitchPath()
	if path != "/custom/path/to/script.sh" {
		t.Errorf("Expected screen switch path '/custom/path/to/script.sh', got '%s'", path)
	}

	// Test empty value falls back to default
	cfg.ScreenSwitchPath = ""
	path = cfg.GetScreenSwitchPath()
	if path != expectedDefault {
		t.Errorf("Expected fallback to default screen switch path '%s', got '%s'", expectedDefault, path)
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	// Save original environment variables
	originalStatusInterval := os.Getenv("MSM_STATUS_UPDATE_INTERVAL")
	originalIPValidation := os.Getenv("MSM_IP_VALIDATION")
	originalDisableCommands := os.Getenv("MSM_DISABLE_COMMANDS")

	// Clean up after test
	defer func() {
		os.Setenv("MSM_STATUS_UPDATE_INTERVAL", originalStatusInterval)
		os.Setenv("MSM_IP_VALIDATION", originalIPValidation)
		os.Setenv("MSM_DISABLE_COMMANDS", originalDisableCommands)
	}()

	cfg := ClientConfig{
		ClientID:             "test-client",
		StatusUpdateInterval: 10 * time.Second,
		DisableCommands:      false,
	}

	// Test status update interval override
	os.Setenv("MSM_STATUS_UPDATE_INTERVAL", "30s")
	cfg.ApplyEnvironmentOverrides()
	if cfg.StatusUpdateInterval != 30*time.Second {
		t.Errorf("Expected status update interval to be overridden to 30s, got %v", cfg.StatusUpdateInterval)
	}

	// Test IP validation override
	os.Setenv("MSM_IP_VALIDATION", "strict")
	cfg.ApplyEnvironmentOverrides()
	if cfg.GetIPValidationMode() != "strict" {
		t.Errorf("Expected IP validation mode to be overridden to 'strict', got '%s'", cfg.GetIPValidationMode())
	}

	// Test disable commands override
	os.Setenv("MSM_DISABLE_COMMANDS", "true")
	cfg.ApplyEnvironmentOverrides()
	if !cfg.DisableCommands {
		t.Error("Expected commands to be disabled via environment override")
	}
}

func TestJSONSerialization(t *testing.T) {
	// Test that status_update_interval is properly serialized/deserialized
	cfg := ClientConfig{
		ClientID:             "550e8400-e29b-41d4-a716-446655440000",
		StatusUpdateInterval: 30 * time.Second,
		DisableCommands:      true,
	}

	// Test JSON marshaling
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Verify the JSON contains the expected field
	jsonStr := string(data)
	if !strings.Contains(jsonStr, "status_update_interval") {
		t.Error("JSON should contain 'status_update_interval' field")
	}
	if !strings.Contains(jsonStr, "30000000000") { // 30 seconds in nanoseconds
		t.Error("JSON should contain the correct duration value")
	}

	// Test JSON unmarshaling
	var newCfg ClientConfig
	err = json.Unmarshal(data, &newCfg)
	if err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify the values are correct
	if newCfg.StatusUpdateInterval != 30*time.Second {
		t.Errorf("Expected status update interval of 30s after unmarshaling, got %v", newCfg.StatusUpdateInterval)
	}
	if newCfg.ClientID != cfg.ClientID {
		t.Errorf("Expected client ID '%s' after unmarshaling, got '%s'", cfg.ClientID, newCfg.ClientID)
	}
	if newCfg.DisableCommands != cfg.DisableCommands {
		t.Errorf("Expected disable commands to be %v after unmarshaling, got %v", cfg.DisableCommands, newCfg.DisableCommands)
	}
}

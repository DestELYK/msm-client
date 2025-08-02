package config

import (
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
		ClientID:            "test-client",
		UpdateInterval:      30,
		StrictIPValidation:  false,
		AllowIPSubnetMatch:  true, // This should be the default
		DisableIPValidation: false,
	}

	mode := cfg.GetIPValidationMode()
	if mode != "subnet" {
		t.Errorf("Default config should use subnet validation, got '%s'", mode)
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
	if codeExpiration != 1*time.Minute {
		t.Errorf("Expected default code expiration of 1 minute, got %v", codeExpiration)
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
	// Test validation with negative security values
	cfg := ClientConfig{
		ClientID:                 "550e8400-e29b-41d4-a716-446655440000", // Valid UUID
		UpdateInterval:           30,
		MaxIPViolations:          -1,
		IPBlacklistDuration:      -1 * time.Hour,
		VerificationCodeLength:   -1,
		VerificationCodeAttempts: -1,
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("Expected validation error for negative security values")
	}

	// Test validation with valid values
	cfg.MaxIPViolations = 5
	cfg.IPBlacklistDuration = 2 * time.Hour
	cfg.VerificationCodeLength = 8
	cfg.VerificationCodeAttempts = 5

	err = ValidateConfig(cfg)
	if err != nil {
		t.Errorf("Expected no validation error, got: %v", err)
	}
}

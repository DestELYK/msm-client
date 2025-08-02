package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/joho/godotenv"
)

type ClientConfig struct {
	ClientID        string `json:"client_id"`
	UpdateInterval  int    `json:"update_interval,omitempty"`  // in seconds
	DisableCommands bool   `json:"disable_commands,omitempty"` // Disable remote command execution

	VerificationCodeLength   int `json:"verification_code_length,omitempty"`   // Length of verification code (default: 6)
	VerificationCodeAttempts int `json:"verification_code_attempts,omitempty"` // Max attempts for verification code (default: 3)

	// Pairing code expiration setting
	PairingCodeExpiration time.Duration `json:"pairing_code_expiration,omitempty"` // How long pairing codes remain valid (default: 1 minute)

	// Screen management settings
	ScreenSwitchPath string `json:"screen_switch_path,omitempty"` // Path to screen switch script (default: /usr/local/bin/mediascreen-installer/scripts/screen-switch.sh)

	// Pairing security settings
	// IP validation modes (in order of precedence):
	// 1. DisableIPValidation: Completely disable IP checking (least secure, most compatible)
	// 2. StrictIPValidation: Require exact IP match (most secure, may fail with NAT/proxies)
	// 3. AllowIPSubnetMatch: Allow same subnet (balanced security and compatibility)
	// 4. Default (none set): Permissive validation (backward compatible)
	StrictIPValidation  bool `json:"strict_ip_validation,omitempty"`  // Require exact IP match for pairing
	AllowIPSubnetMatch  bool `json:"allow_ip_subnet_match,omitempty"` // Allow same subnet for pairing
	DisableIPValidation bool `json:"disable_ip_validation,omitempty"` // Completely disable IP validation

	// IP blacklist security settings
	MaxIPViolations     int           `json:"max_ip_violations,omitempty"`     // Max IP violations before blacklisting (default: 3)
	IPBlacklistDuration time.Duration `json:"ip_blacklist_duration,omitempty"` // How long to blacklist an IP (default: 1 hour)
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
	cfg.UpdateInterval = 30     // Set default update interval
	cfg.DisableCommands = false // Default: commands are enabled

	// Set sensible defaults for IP validation (more permissive for better UX)
	cfg.StrictIPValidation = false  // Don't require exact IP match by default
	cfg.AllowIPSubnetMatch = true   // Allow same subnet by default (good for NAT)
	cfg.DisableIPValidation = false // Keep some validation by default

	// Set default security settings
	cfg.MaxIPViolations = 3                 // Default: 3 violations before blacklisting
	cfg.IPBlacklistDuration = 1 * time.Hour // Default: blacklist for 1 hour

	// Set default verification code settings
	cfg.VerificationCodeLength = 6   // Default: 6 character code
	cfg.VerificationCodeAttempts = 3 // Default: 3 attempts before invalidating

	// Set default pairing code expiration
	cfg.PairingCodeExpiration = 1 * time.Minute // Default: codes expire after 1 minute

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
	if cfg.MaxIPViolations < 0 {
		return errors.New("max_ip_violations must be non-negative")
	}
	if cfg.IPBlacklistDuration < 0 {
		return errors.New("ip_blacklist_duration must be non-negative")
	}
	if cfg.VerificationCodeLength < 0 {
		return errors.New("verification_code_length must be non-negative")
	}
	if cfg.VerificationCodeAttempts < 0 {
		return errors.New("verification_code_attempts must be non-negative")
	}
	if cfg.PairingCodeExpiration < 0 {
		return errors.New("pairing_code_expiration must be non-negative")
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

func LoadEnv() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		fmt.Println("Failed to load .env file, using system environment variables")
	}
}

// ApplyEnvironmentOverrides applies environment variable overrides to the config
func (cfg *ClientConfig) ApplyEnvironmentOverrides() {
	if updateInterval := os.Getenv("MSM_UPDATE_INTERVAL"); updateInterval != "" {
		if val, err := strconv.Atoi(updateInterval); err == nil && val > 0 {
			cfg.UpdateInterval = val
		} else {
			fmt.Printf("Warning: Invalid MSM_UPDATE_INTERVAL value '%s', ignoring\n", updateInterval)
		}
	}

	// Check for IP validation mode override
	if ipValidationMode := os.Getenv("MSM_IP_VALIDATION"); ipValidationMode != "" {
		switch ipValidationMode {
		case "strict":
			cfg.SetStrictIPValidation()
		case "subnet":
			cfg.SetSubnetIPValidation()
		case "permissive":
			cfg.SetPermissiveIPValidation()
		case "disabled":
			cfg.DisableAllIPValidation()
		default:
			fmt.Printf("Warning: Invalid MSM_IP_VALIDATION value '%s', ignoring\n", ipValidationMode)
		}
	}

	// Check for command disable override
	if disableCommands := os.Getenv("MSM_DISABLE_COMMANDS"); disableCommands == "true" || disableCommands == "1" {
		cfg.DisableCommands = true
	}

	// Check for security settings overrides
	if maxViolations := os.Getenv("MSM_MAX_IP_VIOLATIONS"); maxViolations != "" {
		if val, err := strconv.Atoi(maxViolations); err == nil && val >= 0 {
			cfg.MaxIPViolations = val
		} else {
			fmt.Printf("Warning: Invalid MSM_MAX_IP_VIOLATIONS value '%s', ignoring\n", maxViolations)
		}
	}

	if blacklistDuration := os.Getenv("MSM_IP_BLACKLIST_DURATION"); blacklistDuration != "" {
		if duration, err := time.ParseDuration(blacklistDuration); err == nil && duration >= 0 {
			cfg.IPBlacklistDuration = duration
		} else {
			fmt.Printf("Warning: Invalid MSM_IP_BLACKLIST_DURATION value '%s', ignoring\n", blacklistDuration)
		}
	}

	// Check for verification code settings overrides
	if codeLength := os.Getenv("MSM_VERIFICATION_CODE_LENGTH"); codeLength != "" {
		if val, err := strconv.Atoi(codeLength); err == nil && val > 0 {
			cfg.VerificationCodeLength = val
		} else {
			fmt.Printf("Warning: Invalid MSM_VERIFICATION_CODE_LENGTH value '%s', ignoring\n", codeLength)
		}
	}

	if codeAttempts := os.Getenv("MSM_VERIFICATION_CODE_ATTEMPTS"); codeAttempts != "" {
		if val, err := strconv.Atoi(codeAttempts); err == nil && val > 0 {
			cfg.VerificationCodeAttempts = val
		} else {
			fmt.Printf("Warning: Invalid MSM_VERIFICATION_CODE_ATTEMPTS value '%s', ignoring\n", codeAttempts)
		}
	}

	// Check for pairing code expiration override
	if codeExpiration := os.Getenv("MSM_PAIRING_CODE_EXPIRATION"); codeExpiration != "" {
		if duration, err := time.ParseDuration(codeExpiration); err == nil && duration > 0 {
			cfg.PairingCodeExpiration = duration
		} else {
			fmt.Printf("Warning: Invalid MSM_PAIRING_CODE_EXPIRATION value '%s', ignoring\n", codeExpiration)
		}
	}

	// Check for screen switch path override
	if screenSwitchPath := os.Getenv("MSM_SCREEN_SWITCH_PATH"); screenSwitchPath != "" {
		cfg.ScreenSwitchPath = screenSwitchPath
	}
}

// SetStrictIPValidation configures strict IP validation mode
func (cfg *ClientConfig) SetStrictIPValidation() {
	cfg.StrictIPValidation = true
	cfg.AllowIPSubnetMatch = false
	cfg.DisableIPValidation = false
}

// SetSubnetIPValidation configures subnet-based IP validation mode
func (cfg *ClientConfig) SetSubnetIPValidation() {
	cfg.StrictIPValidation = false
	cfg.AllowIPSubnetMatch = true
	cfg.DisableIPValidation = false
}

// SetPermissiveIPValidation configures permissive IP validation mode (default)
func (cfg *ClientConfig) SetPermissiveIPValidation() {
	cfg.StrictIPValidation = false
	cfg.AllowIPSubnetMatch = false
	cfg.DisableIPValidation = false
}

// DisableAllIPValidation completely disables IP validation
func (cfg *ClientConfig) DisableAllIPValidation() {
	cfg.StrictIPValidation = false
	cfg.AllowIPSubnetMatch = false
	cfg.DisableIPValidation = true
}

// GetIPValidationMode returns a string describing the current IP validation mode
func (cfg *ClientConfig) GetIPValidationMode() string {
	if cfg.DisableIPValidation {
		return "disabled"
	}
	if cfg.StrictIPValidation {
		return "strict"
	}
	if cfg.AllowIPSubnetMatch {
		return "subnet"
	}
	return "permissive"
}

// GetMaxIPViolations returns the max IP violations setting with default fallback
func (cfg *ClientConfig) GetMaxIPViolations() int {
	if cfg.MaxIPViolations <= 0 {
		return 3 // Default value
	}
	return cfg.MaxIPViolations
}

// GetIPBlacklistDuration returns the IP blacklist duration with default fallback
func (cfg *ClientConfig) GetIPBlacklistDuration() time.Duration {
	if cfg.IPBlacklistDuration <= 0 {
		return 1 * time.Hour // Default value
	}
	return cfg.IPBlacklistDuration
}

// GetVerificationCodeLength returns the verification code length with default fallback
func (cfg *ClientConfig) GetVerificationCodeLength() int {
	if cfg.VerificationCodeLength <= 0 {
		return 6 // Default value
	}
	return cfg.VerificationCodeLength
}

// GetVerificationCodeAttempts returns the verification code attempts with default fallback
func (cfg *ClientConfig) GetVerificationCodeAttempts() int {
	if cfg.VerificationCodeAttempts <= 0 {
		return 3 // Default value
	}
	return cfg.VerificationCodeAttempts
}

// GetPairingCodeExpiration returns the pairing code expiration with default fallback
func (cfg *ClientConfig) GetPairingCodeExpiration() time.Duration {
	if cfg.PairingCodeExpiration <= 0 {
		return 1 * time.Minute // Default value
	}
	return cfg.PairingCodeExpiration
}

// GetScreenSwitchPath returns the screen switch path with default fallback
func (cfg *ClientConfig) GetScreenSwitchPath() string {
	if cfg.ScreenSwitchPath == "" {
		return "/usr/local/bin/mediascreen-installer/scripts/screen-switch.sh" // Default value
	}
	return cfg.ScreenSwitchPath
}

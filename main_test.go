package main

import (
	"os"
	"path/filepath"
	"testing"

	"msm-client/config"
	"msm-client/state"
)

// setupTestPaths sets up temporary directories for testing
func setupTestPaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Set environment variables to use temp directories
	os.Setenv("MSC_CONFIG_PATH", filepath.Join(tmpDir, "config"))
	os.Setenv("MSC_STATE_PATH", filepath.Join(tmpDir, "state"))
	os.Setenv("MSC_PAIRING_PATH", filepath.Join(tmpDir, "pairing"))
}

func TestConfigValidation(t *testing.T) {
	// Test config validation logic that would be used in main
	tests := []struct {
		name        string
		config      config.ClientConfig
		expectError bool
	}{
		{
			name: "valid config",
			config: config.ClientConfig{
				ClientID:       "550e8400-e29b-41d4-a716-446655440000", // Valid UUID
				DeviceName:     "Test Device",
				UpdateInterval: 30,
			},
			expectError: false,
		},
		{
			name: "missing client ID",
			config: config.ClientConfig{
				DeviceName:     "Test Device",
				UpdateInterval: 30,
			},
			expectError: true,
		},
		{
			name: "invalid update interval",
			config: config.ClientConfig{
				ClientID:       "550e8400-e29b-41d4-a716-446655440000",
				DeviceName:     "Test Device",
				UpdateInterval: 0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := config.ValidateConfig(tt.config)
			if tt.expectError && err == nil {
				t.Fatal("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestStateLifecycle(t *testing.T) {
	setupTestPaths(t)

	// Test the state lifecycle that main.go manages
	testState := state.PairedState{
		ServerWs: "ws://test.example.com/ws",
		Token:    "test-token-12345",
	}

	// Initially no state (fresh start)
	if state.HasState() {
		t.Fatal("State should not exist initially")
	}

	// After successful pairing, state should be saved
	err := state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// State should now exist
	if !state.HasState() {
		t.Fatal("State should exist after pairing")
	}

	// Load state for WebSocket connection
	loadedState, err := state.LoadState()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if loadedState.ServerWs != testState.ServerWs {
		t.Fatalf("Expected ServerWs %s, got %s", testState.ServerWs, loadedState.ServerWs)
	}

	// Simulate state deletion (triggers pairing restart)
	err = state.DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Should trigger return to pairing mode
	if state.HasState() {
		t.Fatal("State should not exist after deletion")
	}
}

func TestConfigCreation(t *testing.T) {
	setupTestPaths(t)

	// Test config creation (what happens on first run)
	cfg, err := config.LoadOrCreateConfig()
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	if cfg.ClientID == "" {
		t.Fatal("ClientID should be generated")
	}

	// Verify config file was created in the temp directory
	configPath := filepath.Join(os.Getenv("MSC_CONFIG_PATH"), "client.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file should be created")
	}

	// Test loading existing config
	cfg2, err := config.LoadOrCreateConfig()
	if err != nil {
		t.Fatalf("Failed to load existing config: %v", err)
	}

	if cfg.ClientID != cfg2.ClientID {
		t.Fatal("ClientID should be consistent across loads")
	}
}

func TestApplicationStates(t *testing.T) {
	setupTestPaths(t)

	// Test the main application state transitions

	// 1. Fresh start - no config, no state
	if state.HasState() {
		t.Fatal("Fresh start should have no state")
	}

	// 2. Config creation
	cfg, err := config.LoadOrCreateConfig()
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	if cfg.ClientID == "" {
		t.Fatal("Config should have ClientID")
	}

	// 3. Pairing phase - no state yet
	if state.HasState() {
		t.Fatal("Should have no state during pairing")
	}

	// 4. Successful pairing - state created
	testState := state.PairedState{
		ServerWs: "ws://test.example.com/ws",
		Token:    "paired-token",
	}
	err = state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state after pairing: %v", err)
	}

	// 5. Normal operation - state exists
	if !state.HasState() {
		t.Fatal("Should have state during normal operation")
	}

	// 6. Connection lost, state deleted - back to pairing
	err = state.DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	if state.HasState() {
		t.Fatal("Should have no state after connection loss")
	}
}

func TestWebSocketConnectionFlow(t *testing.T) {
	setupTestPaths(t)

	// Test the flow that main.go handles for WebSocket connections
	testState := state.PairedState{
		ServerWs: "ws://localhost:8080/ws",
		Token:    "valid-jwt-token",
	}

	// Save state (simulates successful pairing)
	err := state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Load state for connection (what main.go does)
	savedState, err := state.LoadState()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	// Verify connection parameters
	if savedState.ServerWs != testState.ServerWs {
		t.Fatalf("Expected ServerWs %s, got %s", testState.ServerWs, savedState.ServerWs)
	}

	if savedState.Token != testState.Token {
		t.Fatalf("Expected Token %s, got %s", testState.Token, savedState.Token)
	}

	// Simulate connection failure - state gets deleted
	err = state.DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Check if state still exists (monitoring logic)
	if state.HasState() {
		t.Fatal("State should not exist after deletion")
	}

	// This would trigger restart of pairing server
}

func TestConfigValidationInMain(t *testing.T) {
	setupTestPaths(t)

	// Test the config validation that happens in main

	// Create invalid config file in the temp directory
	configPath := filepath.Join(os.Getenv("MSC_CONFIG_PATH"), "client.json")
	err := os.MkdirAll(filepath.Dir(configPath), 0755)
	if err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	invalidConfig := `{"client_id": "", "update_interval": 0}`
	err = os.WriteFile(configPath, []byte(invalidConfig), 0600)
	if err != nil {
		t.Fatalf("Failed to create invalid config: %v", err)
	}

	// Try to load invalid config
	_, err = config.LoadOrCreateConfig()
	if err == nil {
		t.Fatal("Expected error when loading invalid config")
	}
}

func TestPairingToWebSocketTransition(t *testing.T) {
	setupTestPaths(t)

	// Test the transition from pairing to WebSocket that main.go handles

	// 1. Start with no state (pairing mode)
	if state.HasState() {
		t.Fatal("Should start with no state")
	}

	// 2. Successful pairing creates state
	pairedState := state.PairedState{
		ServerWs: "ws://example.com/ws",
		Token:    "pairing-result-token",
	}
	err := state.SaveState(pairedState)
	if err != nil {
		t.Fatalf("Failed to save paired state: %v", err)
	}

	// 3. Check if pairing was successful (what main.go does after pairing server stops)
	if !state.HasState() {
		t.Fatal("State should exist after successful pairing")
	}

	// 4. Load state for WebSocket connection
	loadedState, err := state.LoadState()
	if err != nil {
		t.Fatalf("Failed to load state for WebSocket: %v", err)
	}

	if loadedState.ServerWs != pairedState.ServerWs {
		t.Fatalf("Expected ServerWs %s, got %s", pairedState.ServerWs, loadedState.ServerWs)
	}

	// 5. WebSocket connection established successfully
	// (In real app, this is where WebSocket connection would start)
}

func TestContinuousOperationLoop(t *testing.T) {
	setupTestPaths(t)

	// Test the continuous operation loop logic that main.go implements

	testState := state.PairedState{
		ServerWs: "ws://localhost:8080/ws",
		Token:    "continuous-token",
	}

	// Simulate multiple cycles of connection and reconnection
	for i := 0; i < 3; i++ {
		// Save state
		err := state.SaveState(testState)
		if err != nil {
			t.Fatalf("Cycle %d: Failed to save state: %v", i, err)
		}

		// Verify state exists
		if !state.HasState() {
			t.Fatalf("Cycle %d: State should exist", i)
		}

		// Load state for connection
		_, err = state.LoadState()
		if err != nil {
			t.Fatalf("Cycle %d: Failed to load state: %v", i, err)
		}

		// Simulate connection loss
		err = state.DeleteState()
		if err != nil {
			t.Fatalf("Cycle %d: Failed to delete state: %v", i, err)
		}

		// Verify monitoring detects missing state
		if state.HasState() {
			t.Fatalf("Cycle %d: State should not exist after deletion", i)
		}

		// This would trigger restart (in real app)
	}
}

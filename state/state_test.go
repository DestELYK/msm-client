package state

import (
	"encoding/json"
	"os"
	"testing"
)

// Helper function to set up temporary directories for testing
func setupTestPaths(t *testing.T) (cleanup func()) {
	tempDir := t.TempDir()

	// Store original environment variable
	originalStatePath := os.Getenv("MSC_STATE_PATH")

	// Set environment variable to use temp directory
	os.Setenv("MSC_STATE_PATH", tempDir)

	return func() {
		// Restore original environment variable
		os.Setenv("MSC_STATE_PATH", originalStatePath)
	}
}

func TestSaveAndLoadState(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove(stateFile)

	testState := PairedState{
		ServerWs: "ws://test.example.com/ws",
		Token:    "test-token-123",
	}

	// Test saving state
	err := SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Verify file exists
	if !HasState() {
		t.Fatal("State file should exist after saving")
	}

	// Test loading state
	loadedState, err := LoadState()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if loadedState.ServerWs != testState.ServerWs {
		t.Fatalf("Expected ServerWs %s, got %s", testState.ServerWs, loadedState.ServerWs)
	}
	if loadedState.Token != testState.Token {
		t.Fatalf("Expected Token %s, got %s", testState.Token, loadedState.Token)
	}
}

func TestHasState(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove(stateFile)

	// Initially, no state file should exist
	if HasState() {
		t.Fatal("State file should not exist initially")
	}

	// Create state file
	testState := PairedState{
		ServerWs: "ws://test.example.com/ws",
		Token:    "test-token",
	}
	err := SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Now state should exist
	if !HasState() {
		t.Fatal("State file should exist after saving")
	}
}

func TestDeleteState(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove(stateFile)

	// Create state file first
	testState := PairedState{
		ServerWs: "ws://test.example.com/ws",
		Token:    "test-token",
	}
	err := SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Verify it exists
	if !HasState() {
		t.Fatal("State file should exist after saving")
	}

	// Delete state
	err = DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Verify it no longer exists
	if HasState() {
		t.Fatal("State file should not exist after deletion")
	}
}

func TestDeleteStateFileNotExists(t *testing.T) {
	// Ensure no state file exists
	os.Remove(stateFile)

	// Try to delete non-existent file
	err := DeleteState()
	if err == nil {
		t.Fatal("Expected error when deleting non-existent state file")
	}
}

func TestLoadStateFileNotExists(t *testing.T) {
	// Ensure no state file exists
	os.Remove(stateFile)

	_, err := LoadState()
	if err == nil {
		t.Fatal("Expected error when loading non-existent state file")
	}
}

func TestSaveStateWithEmptyValues(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove(stateFile)

	testState := PairedState{
		ServerWs: "",
		Token:    "",
	}

	err := SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state with empty values: %v", err)
	}

	loadedState, err := LoadState()
	if err != nil {
		t.Fatalf("Failed to load state with empty values: %v", err)
	}

	if loadedState.ServerWs != "" {
		t.Fatalf("Expected empty ServerWs, got %s", loadedState.ServerWs)
	}
	if loadedState.Token != "" {
		t.Fatalf("Expected empty Token, got %s", loadedState.Token)
	}
}

func TestStateFilePermissions(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove(stateFile)

	testState := PairedState{
		ServerWs: "ws://test.example.com/ws",
		Token:    "sensitive-token",
	}

	err := SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Check file permissions
	statePath := getStatePath()
	fileInfo, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("Failed to get file info: %v", err)
	}

	// File should be readable/writable by owner only (0600)
	expectedPerm := os.FileMode(0600)
	if fileInfo.Mode().Perm() != expectedPerm {
		t.Fatalf("Expected file permissions %v, got %v", expectedPerm, fileInfo.Mode().Perm())
	}
}

func TestStateFileCorrupted(t *testing.T) {
	defer os.Remove(stateFile)

	// Create corrupted JSON file
	err := os.WriteFile(stateFile, []byte("invalid json content"), 0600)
	if err != nil {
		t.Fatalf("Failed to create corrupted state file: %v", err)
	}

	_, err = LoadState()
	if err == nil {
		t.Fatal("Expected error when loading corrupted state file")
	}
}

func TestStateJSONFormat(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove(stateFile)

	testState := PairedState{
		ServerWs: "ws://test.example.com/ws",
		Token:    "test-token-123",
	}

	err := SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Read raw file content
	statePath := getStatePath()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state file: %v", err)
	}

	// Verify it's valid JSON and properly formatted
	var rawState map[string]interface{}
	err = json.Unmarshal(data, &rawState)
	if err != nil {
		t.Fatalf("State file contains invalid JSON: %v", err)
	}

	// Verify required fields exist
	if _, exists := rawState["server_ws"]; !exists {
		t.Fatal("State file missing server_ws field")
	}
	if _, exists := rawState["token"]; !exists {
		t.Fatal("State file missing token field")
	}
}

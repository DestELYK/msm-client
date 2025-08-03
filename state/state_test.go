package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadState(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment variable
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tempDir)
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Test data
	testState := PairedState{
		ServerWs:   "ws://example.com:8080/ws",
		SessionKey: "dGVzdF9zZXNzaW9uX2tleQ==", // base64 encoded "test_session_key"
	}

	// Test saving state
	err = SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Verify file was created
	statePath := filepath.Join(tempDir, stateFile)
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatal("State file was not created")
	}

	// Test loading state
	loadedState, err := LoadState()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	// Verify loaded state matches saved state
	if loadedState.ServerWs != testState.ServerWs {
		t.Errorf("Expected ServerWs '%s', got '%s'", testState.ServerWs, loadedState.ServerWs)
	}
	if loadedState.SessionKey != testState.SessionKey {
		t.Errorf("Expected SessionKey '%s', got '%s'", testState.SessionKey, loadedState.SessionKey)
	}
}

func TestSaveStateWithoutSessionKey(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment variable
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tempDir)
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Test data without session key
	testState := PairedState{
		ServerWs: "ws://example.com:8080/ws",
		// SessionKey is omitted
	}

	// Test saving state
	err = SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Test loading state
	loadedState, err := LoadState()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	// Verify loaded state matches saved state
	if loadedState.ServerWs != testState.ServerWs {
		t.Errorf("Expected ServerWs '%s', got '%s'", testState.ServerWs, loadedState.ServerWs)
	}
	if loadedState.SessionKey != "" {
		t.Errorf("Expected empty SessionKey, got '%s'", loadedState.SessionKey)
	}
}

func TestHasState(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment variable
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tempDir)
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Initially, no state should exist
	if HasState() {
		t.Error("Expected HasState() to return false when no state file exists")
	}

	// Create state
	testState := PairedState{
		ServerWs: "ws://example.com:8080/ws",
	}
	err = SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Now state should exist
	if !HasState() {
		t.Error("Expected HasState() to return true when state file exists")
	}
}

func TestDeleteState(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment variable
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tempDir)
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Create state
	testState := PairedState{
		ServerWs: "ws://example.com:8080/ws",
	}
	err = SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Verify state exists
	if !HasState() {
		t.Fatal("State should exist before deletion")
	}

	// Delete state
	err = DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Verify state no longer exists
	if HasState() {
		t.Error("Expected HasState() to return false after deletion")
	}

	// Test deleting non-existent state (should not error)
	err = DeleteState()
	if err != nil {
		t.Fatalf("Deleting non-existent state should not error: %v", err)
	}
}

func TestGetSessionKey(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment variable
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tempDir)
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Test when no state exists
	sessionKey := GetSessionKey()
	if sessionKey != "" {
		t.Errorf("Expected empty session key when no state exists, got '%s'", sessionKey)
	}

	// Create state with session key
	testKey := "dGVzdF9zZXNzaW9uX2tleQ=="
	testState := PairedState{
		ServerWs:   "ws://example.com:8080/ws",
		SessionKey: testKey,
	}
	err = SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Test getting session key
	sessionKey = GetSessionKey()
	if sessionKey != testKey {
		t.Errorf("Expected session key '%s', got '%s'", testKey, sessionKey)
	}

	// Create state without session key
	testStateNoKey := PairedState{
		ServerWs: "ws://example.com:8080/ws",
	}
	err = SaveState(testStateNoKey)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Test getting empty session key
	sessionKey = GetSessionKey()
	if sessionKey != "" {
		t.Errorf("Expected empty session key, got '%s'", sessionKey)
	}
}

func TestHasSessionKey(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment variable
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tempDir)
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Test when no state exists
	if HasSessionKey() {
		t.Error("Expected HasSessionKey() to return false when no state exists")
	}

	// Create state without session key
	testState := PairedState{
		ServerWs: "ws://example.com:8080/ws",
	}
	err = SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Test when session key is empty
	if HasSessionKey() {
		t.Error("Expected HasSessionKey() to return false when session key is empty")
	}

	// Create state with session key
	testStateWithKey := PairedState{
		ServerWs:   "ws://example.com:8080/ws",
		SessionKey: "dGVzdF9zZXNzaW9uX2tleQ==",
	}
	err = SaveState(testStateWithKey)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Test when session key exists
	if !HasSessionKey() {
		t.Error("Expected HasSessionKey() to return true when session key exists")
	}
}

func TestLoadStateFileNotFound(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment variable to non-existent path
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", filepath.Join(tempDir, "nonexistent"))
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Test loading non-existent state
	_, err = LoadState()
	if err == nil {
		t.Error("Expected error when loading non-existent state file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Expected file not found error, got: %v", err)
	}
}

func TestLoadStateInvalidJSON(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment variable
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tempDir)
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Create invalid JSON file
	statePath := filepath.Join(tempDir, stateFile)
	err = os.WriteFile(statePath, []byte("invalid json content"), 0600)
	if err != nil {
		t.Fatalf("Failed to write invalid JSON: %v", err)
	}

	// Test loading invalid JSON
	_, err = LoadState()
	if err == nil {
		t.Error("Expected error when loading invalid JSON")
	}
	if _, ok := err.(*json.SyntaxError); !ok {
		t.Errorf("Expected JSON syntax error, got: %v", err)
	}
}

func TestGetStatePath(t *testing.T) {
	// Test default path
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Unsetenv("MSC_STATE_PATH")
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	defaultStatePath := getStatePath()
	expectedDefault := filepath.Join(defaultPath, stateFile)
	if defaultStatePath != expectedDefault {
		t.Errorf("Expected default path '%s', got '%s'", expectedDefault, defaultStatePath)
	}

	// Test custom path
	customPath := "/custom/test/path"
	os.Setenv("MSC_STATE_PATH", customPath)
	customStatePath := getStatePath()
	expectedCustom := filepath.Join(customPath, stateFile)
	if customStatePath != expectedCustom {
		t.Errorf("Expected custom path '%s', got '%s'", expectedCustom, customStatePath)
	}
}

func TestJSONSerialization(t *testing.T) {
	// Test that PairedState serializes/deserializes correctly
	testState := PairedState{
		ServerWs:   "ws://example.com:8080/ws",
		SessionKey: "dGVzdF9zZXNzaW9uX2tleQ==",
	}

	// Test JSON marshaling
	data, err := json.Marshal(testState)
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Verify JSON structure
	var jsonMap map[string]interface{}
	err = json.Unmarshal(data, &jsonMap)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON map: %v", err)
	}

	if jsonMap["server_ws"] != testState.ServerWs {
		t.Errorf("JSON server_ws mismatch: expected '%s', got '%v'", testState.ServerWs, jsonMap["server_ws"])
	}
	if jsonMap["session_key"] != testState.SessionKey {
		t.Errorf("JSON session_key mismatch: expected '%s', got '%v'", testState.SessionKey, jsonMap["session_key"])
	}

	// Test JSON unmarshaling
	var newState PairedState
	err = json.Unmarshal(data, &newState)
	if err != nil {
		t.Fatalf("Failed to unmarshal state: %v", err)
	}

	if newState.ServerWs != testState.ServerWs {
		t.Errorf("Unmarshaled ServerWs mismatch: expected '%s', got '%s'", testState.ServerWs, newState.ServerWs)
	}
	if newState.SessionKey != testState.SessionKey {
		t.Errorf("Unmarshaled SessionKey mismatch: expected '%s', got '%s'", testState.SessionKey, newState.SessionKey)
	}
}

func TestSaveStateCreateDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "msm-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set environment variable to nested path that doesn't exist
	nestedPath := filepath.Join(tempDir, "nested", "deep", "path")
	originalPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", nestedPath)
	defer os.Setenv("MSC_STATE_PATH", originalPath)

	// Verify the nested path doesn't exist
	if _, err := os.Stat(nestedPath); !os.IsNotExist(err) {
		t.Fatal("Nested path should not exist initially")
	}

	// Save state (should create the directory)
	testState := PairedState{
		ServerWs: "ws://example.com:8080/ws",
	}
	err = SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Verify the directory was created
	if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
		t.Error("Expected directory to be created")
	}

	// Verify the state file was created
	statePath := filepath.Join(nestedPath, stateFile)
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Error("Expected state file to be created")
	}
}

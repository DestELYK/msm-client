package pairing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"msm-client/config"
	"msm-client/state"
)

// Helper function to clean up between tests
func cleanupTest() {
	StopPairingServer()
	ClearAllCallbacks()
	ResetPairing()
	os.Remove(pairingCodeFile)
	os.Remove("paired.json")
	state.DeleteState()
	time.Sleep(100 * time.Millisecond) // Allow cleanup to complete
}

// Helper function to set up temporary directories for testing
func setupTestPaths(t *testing.T) (cleanup func()) {
	tempDir := t.TempDir()

	// Store original environment variables
	originalConfigPath := os.Getenv("MSC_CONFIG_PATH")
	originalStatePath := os.Getenv("MSC_STATE_PATH")
	originalPairingPath := os.Getenv("MSC_PAIRING_PATH")

	// Set environment variables to use temp directory
	os.Setenv("MSC_CONFIG_PATH", tempDir)
	os.Setenv("MSC_STATE_PATH", tempDir)
	os.Setenv("MSC_PAIRING_PATH", tempDir)

	return func() {
		// Restore original environment variables
		os.Setenv("MSC_CONFIG_PATH", originalConfigPath)
		os.Setenv("MSC_STATE_PATH", originalStatePath)
		os.Setenv("MSC_PAIRING_PATH", originalPairingPath)
	}
}

// Helper function to find an available port
func findAvailablePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// Helper to create a test server with custom port
func startTestServer(cfg config.ClientConfig, port int) {
	// Clear any existing server first
	StopPairingServer()
	time.Sleep(50 * time.Millisecond) // Allow cleanup to complete

	go func() {
		StartPairingServerOnPort(cfg, port)
	}()

	// Give server time to start and wait for it to be ready
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	for i := 0; i < 50; i++ { // Wait up to 5 seconds
		time.Sleep(100 * time.Millisecond)

		// Try a simple connection to see if server is ready
		if IsServerRunning() {
			// Also test if the server is actually accepting connections
			resp, err := http.Get(baseURL + "/pair")
			if err == nil {
				resp.Body.Close()
				break // Server is ready
			}
		}

		if i == 49 {
			panic(fmt.Sprintf("Server failed to start on port %d after 5 seconds", port))
		}
	}
}

func TestStartPairingServerCallbacks(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	// Clean up
	defer os.Remove(pairingCodeFile)
	defer os.Remove("paired.json")
	defer state.DeleteState()
	defer ClearAllCallbacks()

	// Set up environment
	os.Setenv("MSM_SECRET_KEY", "test-secret-key")
	defer os.Unsetenv("MSM_SECRET_KEY")

	cfg := config.ClientConfig{
		ClientID: "test-client-callbacks",
	}

	// Set up callback tracking
	var callbackEvents []string
	var mu sync.Mutex

	addEvent := func(event string) {
		mu.Lock()
		callbackEvents = append(callbackEvents, event)
		mu.Unlock()
	}

	// Set up all callbacks
	SetOnServerStarted(func(addr string) {
		addEvent(fmt.Sprintf("server_started:%s", addr))
	})

	SetOnPairingStarted(func(code string, expiry time.Time) {
		addEvent(fmt.Sprintf("pairing_started:%s", code))
	})

	SetOnPairingSuccess(func(serverWs, token string) {
		addEvent(fmt.Sprintf("pairing_success:%s", serverWs))
	})

	SetOnPairingFailed(func(reason string, failCount int) {
		addEvent(fmt.Sprintf("pairing_failed:%s:%d", reason, failCount))
	})

	SetOnServerStopped(func() {
		addEvent("server_stopped")
	})

	// Find available port and start server manually for testing
	port, err := findAvailablePort()
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}

	// Create test server
	startTestServer(cfg, port)
	defer StopPairingServer()

	baseURL := fmt.Sprintf("http://localhost:%d", port)

	// Test pairing flow with callbacks
	t.Run("PairingFlow", func(t *testing.T) {
		// Step 1: Request pairing code
		resp, err := http.Post(baseURL+"/pair", "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to request pairing: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		// Wait for callbacks
		time.Sleep(100 * time.Millisecond)

		// Get the generated pairing code
		pairingCode, _ := GetPairingCode()
		if pairingCode == "" {
			t.Fatal("Pairing code should be generated")
		}

		// Step 2: Test wrong code (should trigger failure callback)
		wrongReq := map[string]string{
			"code":     "WRONG123",
			"serverWs": "ws://test.example.com/ws",
		}
		wrongBody, _ := json.Marshal(wrongReq)

		resp, err = http.Post(baseURL+"/pair/confirm", "application/json", bytes.NewReader(wrongBody))
		if err != nil {
			t.Fatalf("Failed to send wrong code: %v", err)
		}
		resp.Body.Close()

		// Wait for callback
		time.Sleep(100 * time.Millisecond)

		// Step 3: Test correct code (should trigger success callback)
		correctReq := map[string]string{
			"code":     pairingCode,
			"serverWs": "ws://test.example.com/ws",
		}
		correctBody, _ := json.Marshal(correctReq)

		resp, err = http.Post(baseURL+"/pair/confirm", "application/json", bytes.NewReader(correctBody))
		if err != nil {
			t.Fatalf("Failed to send correct code: %v", err)
		}
		resp.Body.Close()

		// Wait for callbacks
		time.Sleep(200 * time.Millisecond)

		// Verify callbacks were triggered
		mu.Lock()
		events := callbackEvents[:]
		mu.Unlock()

		t.Logf("Callback events: %v", events)

		// Check for expected events
		hasStarted := false
		hasPairingStarted := false
		hasPairingFailed := false
		hasPairingSuccess := false

		for _, event := range events {
			if event == fmt.Sprintf("pairing_started:%s", pairingCode) {
				hasPairingStarted = true
			}
			if event == "pairing_failed:incorrect_code:1" {
				hasPairingFailed = true
			}
			if event == "pairing_success:ws://test.example.com/ws" {
				hasPairingSuccess = true
			}
			if event == "server_started" || len(event) > 15 && event[:14] == "server_started" {
				hasStarted = true
			}
		}

		if !hasStarted {
			t.Error("Expected server_started callback")
		}
		if !hasPairingStarted {
			t.Error("Expected pairing_started callback")
		}
		if !hasPairingFailed {
			t.Error("Expected pairing_failed callback for wrong code")
		}
		if !hasPairingSuccess {
			t.Error("Expected pairing_success callback")
		}

		// Verify state was saved
		if !state.HasState() {
			t.Error("State should be saved after successful pairing")
		}
	})
}

func TestStartPairingServerWithRealServer(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	// Clean up
	defer os.Remove(pairingCodeFile)
	defer os.Remove("paired.json")
	defer state.DeleteState()
	defer ClearAllCallbacks()

	// Set up environment
	os.Setenv("MSM_SECRET_KEY", "test-secret-key")
	defer os.Unsetenv("MSM_SECRET_KEY")

	cfg := config.ClientConfig{
		ClientID: "test-real-server",
	}

	// Find available port and create custom server for testing
	port, err := findAvailablePort()
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}

	startTestServer(cfg, port)
	defer StopPairingServer()

	baseURL := fmt.Sprintf("http://localhost:%d", port)

	// Verify server is running
	if !IsServerRunning() {
		t.Fatal("Server should be running")
	}

	// Test complete pairing workflow
	t.Run("CompleteWorkflow", func(t *testing.T) {
		// Reset pairing state
		ResetPairing()

		// Step 1: Generate pairing code
		resp, err := http.Post(baseURL+"/pair", "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to request pairing: %v", err)
		}
		defer resp.Body.Close()

		var pairResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&pairResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Verify response
		if pairResp["message"] == nil {
			t.Error("Expected message in pairing response")
		}

		// Get code and verify it was generated
		code, expiry := GetPairingCode()
		if code == "" {
			t.Fatal("Pairing code should be generated")
		}
		if expiry.IsZero() {
			t.Fatal("Expiry should be set")
		}

		t.Logf("Generated code: %s, expires: %s", code, expiry)

		// Step 2: Confirm pairing
		confirmReq := map[string]string{
			"code":     code,
			"serverWs": "ws://localhost:8080/ws",
		}
		confirmBody, _ := json.Marshal(confirmReq)

		resp, err = http.Post(baseURL+"/pair/confirm", "application/json", bytes.NewReader(confirmBody))
		if err != nil {
			t.Fatalf("Failed to confirm pairing: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var confirmResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&confirmResp); err != nil {
			t.Fatalf("Failed to decode confirm response: %v", err)
		}

		// Verify response
		if confirmResp["message"] != "paired" {
			t.Errorf("Expected 'paired' message, got %v", confirmResp["message"])
		}
		if confirmResp["token"] == nil {
			t.Error("Expected token in response")
		}

		// Verify state was saved
		if !state.HasState() {
			t.Fatal("State should be saved")
		}

		savedState, err := state.LoadState()
		if err != nil {
			t.Fatalf("Failed to load state: %v", err)
		}
		if savedState.ServerWs != "ws://localhost:8080/ws" {
			t.Errorf("Expected ServerWs 'ws://localhost:8080/ws', got %s", savedState.ServerWs)
		}
	})
}

func TestStartPairingServerErrorCases(t *testing.T) {
	cleanupTest()
	defer cleanupTest()

	cfg := config.ClientConfig{
		ClientID: "test-errors",
	}

	t.Run("InvalidJSON", func(t *testing.T) {
		// Find available port for this subtest
		port, err := findAvailablePort()
		if err != nil {
			t.Fatalf("Failed to find available port: %v", err)
		}

		startTestServer(cfg, port)
		defer StopPairingServer()

		baseURL := fmt.Sprintf("http://localhost:%d", port)

		// Test invalid JSON
		resp, err := http.Post(baseURL+"/pair/confirm", "application/json", bytes.NewReader([]byte("invalid json")))
		if err != nil {
			t.Fatalf("Failed to send invalid JSON: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("ExpiredCode", func(t *testing.T) {
		// Find available port for this subtest
		port, err := findAvailablePort()
		if err != nil {
			t.Fatalf("Failed to find available port: %v", err)
		}

		startTestServer(cfg, port)
		defer StopPairingServer()

		baseURL := fmt.Sprintf("http://localhost:%d", port)

		// Set expired code manually
		codeMutex.Lock()
		pairCode = "EXPIRED123"
		expiry = time.Now().Add(-1 * time.Minute)
		failCount = 0
		codeMutex.Unlock()

		confirmReq := map[string]string{
			"code":     "EXPIRED123",
			"serverWs": "ws://test.example.com/ws",
		}
		confirmBody, _ := json.Marshal(confirmReq)

		resp, err := http.Post(baseURL+"/pair/confirm", "application/json", bytes.NewReader(confirmBody))
		if err != nil {
			t.Fatalf("Failed to send expired code: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})
}

func TestCallbackFunctions(t *testing.T) {
	defer ClearAllCallbacks()

	// Test setting and clearing callbacks
	called := false

	SetOnPairingStarted(func(code string, expiry time.Time) {
		called = true
	})

	// Trigger callback
	triggerOnPairingStarted("TEST123", time.Now())

	if !called {
		t.Error("Callback should have been called")
	}

	// Clear callbacks
	ClearAllCallbacks()
	called = false

	// Trigger again - should not be called
	triggerOnPairingStarted("TEST456", time.Now())

	if called {
		t.Error("Callback should not have been called after clearing")
	}
}

func TestGlobalServerState(t *testing.T) {
	// Test global state management
	defer ClearAllCallbacks()

	// Initially no server
	if IsServerRunning() {
		t.Error("No server should be running initially")
	}

	// Test configuration
	cfg := config.ClientConfig{
		ClientID: "test-global-state",
	}

	SetConfig(cfg)
	retrievedCfg := GetConfig()
	if retrievedCfg.ClientID != cfg.ClientID {
		t.Errorf("Expected ClientID %s, got %s", cfg.ClientID, retrievedCfg.ClientID)
	}
}

func TestStopPairingServerCallback(t *testing.T) {
	defer ClearAllCallbacks()

	// Set up callback
	var stopped bool
	SetOnServerStopped(func() {
		stopped = true
	})

	// Call StopPairingServer when no server is running (should be safe)
	StopPairingServer()

	// Note: The callback is only triggered when there's actually a server to stop
	// So this tests the safe calling of StopPairingServer
	_ = stopped // Acknowledge variable (callback won't be triggered without actual server)
}

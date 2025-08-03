package pairing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"msm-client/config"
	"msm-client/state"
)

func TestNewPairingManager(t *testing.T) {
	pm := NewPairingManager()

	if pm == nil {
		t.Fatal("NewPairingManager() returned nil")
	}

	if pm.ipBlacklist == nil {
		t.Error("ipBlacklist map not initialized")
	}

	if pm.ipViolations == nil {
		t.Error("ipViolations map not initialized")
	}

	if pm.display == nil {
		t.Error("display manager not initialized")
	}

	// Test initial state
	if pm.IsServerRunning() {
		t.Error("Server should not be running initially")
	}

	code, expiry := pm.GetPairingCode()
	if code != "" || !expiry.IsZero() {
		t.Error("No pairing code should exist initially")
	}
}

func TestConfigurationManagement(t *testing.T) {
	pm := NewPairingManager()

	// Test default config
	cfg := pm.GetConfig()
	if cfg.ClientID != "" {
		t.Error("Default config should have empty ClientID")
	}

	// Test setting config
	testConfig := config.ClientConfig{
		ClientID:                 "test-client-123",
		StatusUpdateInterval:     30 * time.Second,
		VerificationCodeLength:   8,
		VerificationCodeAttempts: 5,
		MaxIPViolations:          5,
	}

	pm.SetConfig(testConfig)
	retrievedConfig := pm.GetConfig()

	if retrievedConfig.ClientID != testConfig.ClientID {
		t.Errorf("Expected ClientID %s, got %s", testConfig.ClientID, retrievedConfig.ClientID)
	}

	if retrievedConfig.VerificationCodeLength != testConfig.VerificationCodeLength {
		t.Errorf("Expected VerificationCodeLength %d, got %d", testConfig.VerificationCodeLength, retrievedConfig.VerificationCodeLength)
	}
}

func TestCallbackManagement(t *testing.T) {
	pm := NewPairingManager()

	// Test callback setting and clearing
	var pairingStartedCalled bool
	var pairingSuccessCalled bool
	var pairingFailedCalled bool
	var serverStartedCalled bool
	var serverStoppedCalled bool

	pm.SetOnPairingStarted(func(code string, expiry time.Time) {
		pairingStartedCalled = true
	})

	pm.SetOnPairingSuccess(func(serverWs string) {
		pairingSuccessCalled = true
	})

	pm.SetOnPairingFailed(func(reason string, failCount int) {
		pairingFailedCalled = true
	})

	pm.SetOnServerStarted(func(addr string) {
		serverStartedCalled = true
	})

	pm.SetOnServerStopped(func() {
		serverStoppedCalled = true
	})

	// Trigger callbacks
	pm.triggerOnPairingStarted("123456", time.Now())
	pm.triggerOnPairingSuccess("ws://test")
	pm.triggerOnPairingFailed("test", 1)
	pm.triggerOnServerStarted(":8080")
	pm.triggerOnServerStopped()

	if !pairingStartedCalled {
		t.Error("OnPairingStarted callback not called")
	}
	if !pairingSuccessCalled {
		t.Error("OnPairingSuccess callback not called")
	}
	if !pairingFailedCalled {
		t.Error("OnPairingFailed callback not called")
	}
	if !serverStartedCalled {
		t.Error("OnServerStarted callback not called")
	}
	if !serverStoppedCalled {
		t.Error("OnServerStopped callback not called")
	}

	// Test clearing callbacks
	pm.ClearAllCallbacks()

	// Reset flags
	pairingStartedCalled = false
	pairingSuccessCalled = false

	// Trigger callbacks again - should not be called
	pm.triggerOnPairingStarted("123456", time.Now())
	pm.triggerOnPairingSuccess("ws://test")

	if pairingStartedCalled || pairingSuccessCalled {
		t.Error("Callbacks should not be called after clearing")
	}
}

func TestPairingCodeManagement(t *testing.T) {
	pm := NewPairingManager()

	// Set up temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "pairing_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("MSC_PAIRING_PATH", tmpDir)
	defer os.Unsetenv("MSC_PAIRING_PATH")

	// Test saving pairing code
	testCode := "123456"
	err = pm.SavePairingCode(testCode)
	if err != nil {
		t.Errorf("Failed to save pairing code: %v", err)
	}

	// Test loading pairing code
	loadedCode, err := pm.LoadPairingCode()
	if err != nil {
		t.Errorf("Failed to load pairing code: %v", err)
	}

	if loadedCode != testCode {
		t.Errorf("Expected loaded code %s, got %s", testCode, loadedCode)
	}

	// Test deleting pairing code
	err = pm.DeletePairingCode()
	if err != nil {
		t.Errorf("Failed to delete pairing code: %v", err)
	}

	// Code should no longer exist
	loadedCode, err = pm.LoadPairingCode()
	if err == nil && loadedCode != "" {
		t.Error("Expected pairing code to be deleted")
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name         string
		setupRequest func() *http.Request
		expectedIP   string
		description  string
	}{
		{
			name: "X-Forwarded-For header",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Forwarded-For", "192.168.1.100, 10.0.0.1")
				req.RemoteAddr = "127.0.0.1:12345"
				return req
			},
			expectedIP:  "192.168.1.100",
			description: "Should extract first IP from X-Forwarded-For header",
		},
		{
			name: "X-Real-IP header",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Real-IP", "192.168.1.200")
				req.RemoteAddr = "127.0.0.1:12345"
				return req
			},
			expectedIP:  "192.168.1.200",
			description: "Should extract IP from X-Real-IP header",
		},
		{
			name: "Remote address fallback",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.RemoteAddr = "192.168.1.300:54321"
				return req
			},
			expectedIP:  "192.168.1.300",
			description: "Should fall back to remote address",
		},
		{
			name: "Invalid X-Forwarded-For",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Forwarded-For", "invalid-ip")
				req.RemoteAddr = "192.168.1.400:12345"
				return req
			},
			expectedIP:  "192.168.1.400",
			description: "Should fall back to remote address when X-Forwarded-For is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupRequest()
			ip := getClientIP(req)

			if ip != tt.expectedIP {
				t.Errorf("%s: expected IP %s, got %s", tt.description, tt.expectedIP, ip)
			}
		})
	}
}

func TestBlacklistManagement(t *testing.T) {
	pm := NewPairingManager()

	cfg := config.ClientConfig{
		MaxIPViolations:     3,
		IPBlacklistDuration: 10 * time.Minute,
	}
	pm.SetConfig(cfg)

	testIP := "192.168.1.100"

	// Test initial state
	if pm.isIPBlacklisted(testIP) {
		t.Error("IP should not be blacklisted initially")
	}

	// Record violations
	for i := 0; i < 2; i++ {
		blacklisted := pm.recordIPViolation(testIP)
		if blacklisted {
			t.Errorf("IP should not be blacklisted after %d violations", i+1)
		}
		if pm.isIPBlacklisted(testIP) {
			t.Errorf("IP should not be blacklisted after %d violations", i+1)
		}
	}

	// Third violation should trigger blacklist
	blacklisted := pm.recordIPViolation(testIP)
	if !blacklisted {
		t.Error("IP should be blacklisted after 3 violations")
	}

	if !pm.isIPBlacklisted(testIP) {
		t.Error("IP should be blacklisted after 3 violations")
	}

	// Test blacklist status
	status := pm.GetBlacklistStatus()
	if _, exists := status[testIP]; !exists {
		t.Error("IP should appear in blacklist status")
	}

	// Test clearing blacklist
	pm.ClearBlacklist()
	if pm.isIPBlacklisted(testIP) {
		t.Error("IP should not be blacklisted after clearing")
	}

	status = pm.GetBlacklistStatus()
	if len(status) != 0 {
		t.Error("Blacklist status should be empty after clearing")
	}
}

func TestHandlePair(t *testing.T) {
	pm := NewPairingManager()

	cfg := config.ClientConfig{
		VerificationCodeLength: 6,
		PairingCodeExpiration:  1 * time.Minute,
	}
	pm.SetConfig(cfg)

	handler := pm.HandlePair(cfg)

	t.Run("Generate new pairing code", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/pair", nil)
		req.RemoteAddr = "192.168.1.100:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}

		if message, ok := response["message"].(string); !ok || message == "" {
			t.Error("Response should contain a message")
		}

		if expiry, ok := response["expiry"].(string); !ok || expiry == "" {
			t.Error("Response should contain an expiry timestamp")
		}

		// Verify pairing code was generated
		code, _ := pm.GetPairingCode()
		if code == "" {
			t.Error("Pairing code should be generated")
		}

		if len(code) != 6 {
			t.Errorf("Expected code length 6, got %d", len(code))
		}
	})

	t.Run("Return existing code when valid", func(t *testing.T) {
		// First request should generate a code
		req1 := httptest.NewRequest("GET", "/pair", nil)
		req1.RemoteAddr = "192.168.1.100:12345"
		rr1 := httptest.NewRecorder()
		handler.ServeHTTP(rr1, req1)

		originalCode, _ := pm.GetPairingCode()

		// Second request should return the same code
		req2 := httptest.NewRequest("GET", "/pair", nil)
		req2.RemoteAddr = "192.168.1.100:12345"
		rr2 := httptest.NewRecorder()
		handler.ServeHTTP(rr2, req2)

		if status := rr2.Code; status != http.StatusOK {
			t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		currentCode, _ := pm.GetPairingCode()
		if currentCode != originalCode {
			t.Error("Code should remain the same for subsequent requests")
		}
	})

	t.Run("Blacklisted IP rejection", func(t *testing.T) {
		blacklistedIP := "192.168.1.999"

		// Manually blacklist the IP
		pm.blacklistMutex.Lock()
		pm.ipBlacklist[blacklistedIP] = time.Now().Add(1 * time.Hour)
		pm.blacklistMutex.Unlock()

		req := httptest.NewRequest("GET", "/pair", nil)
		req.RemoteAddr = blacklistedIP + ":12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusForbidden {
			t.Errorf("Handler should return 403 for blacklisted IP, got %v", status)
		}
	})
}

func TestHandleConfirm(t *testing.T) {
	pm := NewPairingManager()

	cfg := config.ClientConfig{
		VerificationCodeLength:   6,
		VerificationCodeAttempts: 3,
		PairingCodeExpiration:    1 * time.Minute,
		AllowIPSubnetMatch:       true,
	}
	pm.SetConfig(cfg)

	// Set up temporary directory for state
	tmpDir, err := os.MkdirTemp("", "pairing_confirm_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("MSC_STATE_PATH", tmpDir)
	defer os.Unsetenv("MSC_STATE_PATH")

	handler := pm.HandleConfirm(cfg)

	// Generate a test pairing code
	testCode := "123456"
	pm.codeMutex.Lock()
	pm.pairCode = testCode
	pm.pairCodeIP = "192.168.1.100"
	pm.expiry = time.Now().Add(1 * time.Minute)
	pm.failCount = 0
	pm.codeMutex.Unlock()

	t.Run("Successful pairing", func(t *testing.T) {
		requestBody := map[string]string{
			"code":     testCode,
			"serverWs": "ws://test-server:8080/ws",
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/pair/confirm", bytes.NewReader(jsonBody))
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Handler returned wrong status code: got %v want %v, body: %s", status, http.StatusOK, rr.Body.String())
		}

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		if err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}

		if message, ok := response["message"].(string); !ok || message != "paired" {
			t.Errorf("Expected message 'paired', got %v", message)
		}

		// Verify state was saved
		if !state.HasState() {
			t.Error("State should be saved after successful pairing")
		}

		savedState, err := state.LoadState()
		if err != nil {
			t.Errorf("Failed to load saved state: %v", err)
		}

		if savedState.ServerWs != requestBody["serverWs"] {
			t.Errorf("Expected server WS %s, got %s", requestBody["serverWs"], savedState.ServerWs)
		}
	})

	t.Run("Invalid request format", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/pair/confirm", strings.NewReader("invalid json"))
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("Handler should return 400 for invalid JSON, got %v", status)
		}
	})

	t.Run("Incorrect code", func(t *testing.T) {
		// Reset pairing code for this test
		pm.codeMutex.Lock()
		pm.pairCode = testCode
		pm.pairCodeIP = "192.168.1.100"
		pm.expiry = time.Now().Add(1 * time.Minute)
		pm.failCount = 0
		pm.codeMutex.Unlock()

		requestBody := map[string]string{
			"code":     "wrong123",
			"serverWs": "ws://test-server:8080/ws",
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/pair/confirm", bytes.NewReader(jsonBody))
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusUnauthorized {
			t.Errorf("Handler should return 401 for incorrect code, got %v", status)
		}

		// Verify fail count increased
		_, _, failCount := pm.GetPairingStatus()
		if failCount != 1 {
			t.Errorf("Expected fail count 1, got %d", failCount)
		}
	})

	t.Run("Expired code", func(t *testing.T) {
		// Set expired code
		pm.codeMutex.Lock()
		pm.pairCode = testCode
		pm.pairCodeIP = "192.168.1.100"
		pm.expiry = time.Now().Add(-1 * time.Minute) // Expired
		pm.failCount = 0
		pm.codeMutex.Unlock()

		requestBody := map[string]string{
			"code":     testCode,
			"serverWs": "ws://test-server:8080/ws",
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/pair/confirm", bytes.NewReader(jsonBody))
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusForbidden {
			t.Errorf("Handler should return 403 for expired code, got %v", status)
		}
	})

	t.Run("Blacklisted IP rejection", func(t *testing.T) {
		blacklistedIP := "192.168.1.888"

		// Manually blacklist the IP
		pm.blacklistMutex.Lock()
		pm.ipBlacklist[blacklistedIP] = time.Now().Add(1 * time.Hour)
		pm.blacklistMutex.Unlock()

		requestBody := map[string]string{
			"code":     testCode,
			"serverWs": "ws://test-server:8080/ws",
		}

		jsonBody, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/pair/confirm", bytes.NewReader(jsonBody))
		req.RemoteAddr = blacklistedIP + ":12345"
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusForbidden {
			t.Errorf("Handler should return 403 for blacklisted IP, got %v", status)
		}
	})
}

func TestValidateCode(t *testing.T) {
	pm := NewPairingManager()

	cfg := config.ClientConfig{
		VerificationCodeAttempts: 3,
	}
	pm.SetConfig(cfg)

	testCode := "123456"

	t.Run("Valid code validation", func(t *testing.T) {
		pm.codeMutex.Lock()
		pm.pairCode = testCode
		pm.expiry = time.Now().Add(1 * time.Minute)
		pm.failCount = 0
		pm.codeMutex.Unlock()

		result := pm.ValidateCode(testCode)
		if !result {
			t.Error("Valid code should pass validation")
		}
	})

	t.Run("Invalid code validation", func(t *testing.T) {
		pm.codeMutex.Lock()
		pm.pairCode = testCode
		pm.expiry = time.Now().Add(1 * time.Minute)
		pm.failCount = 0
		pm.codeMutex.Unlock()

		result := pm.ValidateCode("wrong")
		if result {
			t.Error("Invalid code should fail validation")
		}

		// Verify fail count increased
		if pm.failCount != 1 {
			t.Errorf("Expected fail count 1, got %d", pm.failCount)
		}
	})

	t.Run("Expired code validation", func(t *testing.T) {
		pm.codeMutex.Lock()
		pm.pairCode = testCode
		pm.expiry = time.Now().Add(-1 * time.Minute) // Expired
		pm.failCount = 0
		pm.codeMutex.Unlock()

		result := pm.ValidateCode(testCode)
		if result {
			t.Error("Expired code should fail validation")
		}
	})

	t.Run("Max attempts exceeded", func(t *testing.T) {
		pm.codeMutex.Lock()
		pm.pairCode = testCode
		pm.expiry = time.Now().Add(1 * time.Minute)
		pm.failCount = 3 // Max attempts reached
		pm.codeMutex.Unlock()

		result := pm.ValidateCode(testCode)
		if result {
			t.Error("Code should fail validation when max attempts exceeded")
		}
	})
}

func TestResetPairing(t *testing.T) {
	pm := NewPairingManager()

	// Set up temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "pairing_reset_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("MSC_PAIRING_PATH", tmpDir)
	defer os.Unsetenv("MSC_PAIRING_PATH")

	// Set up pairing state
	testCode := "123456"
	pm.codeMutex.Lock()
	pm.pairCode = testCode
	pm.pairCodeIP = "192.168.1.100"
	pm.expiry = time.Now().Add(1 * time.Minute)
	pm.failCount = 2
	pm.codeMutex.Unlock()

	// Save pairing code file
	err = pm.SavePairingCode(testCode)
	if err != nil {
		t.Errorf("Failed to save pairing code: %v", err)
	}

	// Reset pairing
	pm.ResetPairing()

	// Verify everything is cleared
	code, expiry := pm.GetPairingCode()
	if code != "" || !expiry.IsZero() {
		t.Error("Pairing code should be cleared after reset")
	}

	if pm.pairCodeIP != "" {
		t.Error("Pair code IP should be cleared after reset")
	}

	if pm.failCount != 0 {
		t.Error("Fail count should be reset to 0")
	}

	// Verify file is deleted
	_, err = pm.LoadPairingCode()
	if err == nil {
		t.Error("Pairing code file should be deleted after reset")
	}
}

func TestGetPairingStatus(t *testing.T) {
	pm := NewPairingManager()

	cfg := config.ClientConfig{
		VerificationCodeAttempts: 3,
	}
	pm.SetConfig(cfg)

	t.Run("Valid status", func(t *testing.T) {
		testCode := "123456"
		testExpiry := time.Now().Add(1 * time.Minute)

		pm.codeMutex.Lock()
		pm.pairCode = testCode
		pm.expiry = testExpiry
		pm.failCount = 1
		pm.codeMutex.Unlock()

		code, expiry, failCount := pm.GetPairingStatus()

		if code != testCode {
			t.Errorf("Expected code %s, got %s", testCode, code)
		}

		if !expiry.Equal(testExpiry) {
			t.Errorf("Expected expiry %v, got %v", testExpiry, expiry)
		}

		if failCount != 1 {
			t.Errorf("Expected fail count 1, got %d", failCount)
		}
	})

	t.Run("Expired status", func(t *testing.T) {
		pm.codeMutex.Lock()
		pm.pairCode = "123456"
		pm.expiry = time.Now().Add(-1 * time.Minute) // Expired
		pm.failCount = 1
		pm.codeMutex.Unlock()

		code, expiry, failCount := pm.GetPairingStatus()

		if code != "expired" {
			t.Errorf("Expected status 'expired', got %s", code)
		}

		if !expiry.IsZero() {
			t.Error("Expiry should be zero for expired status")
		}

		if failCount != 1 {
			t.Errorf("Expected fail count 1, got %d", failCount)
		}
	})

	t.Run("Max attempts exceeded", func(t *testing.T) {
		pm.codeMutex.Lock()
		pm.pairCode = "123456"
		pm.expiry = time.Now().Add(1 * time.Minute)
		pm.failCount = 3 // Max attempts
		pm.codeMutex.Unlock()

		code, expiry, failCount := pm.GetPairingStatus()

		if code != "expired" {
			t.Errorf("Expected status 'expired', got %s", code)
		}

		if !expiry.IsZero() {
			t.Error("Expiry should be zero when max attempts exceeded")
		}

		if failCount != 3 {
			t.Errorf("Expected fail count 3, got %d", failCount)
		}
	})
}

func TestCleanupBlacklist(t *testing.T) {
	pm := NewPairingManager()

	// Add some blacklist entries with different expiry times
	now := time.Now()

	pm.blacklistMutex.Lock()
	pm.ipBlacklist["192.168.1.100"] = now.Add(-1 * time.Hour)    // Expired
	pm.ipBlacklist["192.168.1.101"] = now.Add(1 * time.Hour)     // Valid
	pm.ipBlacklist["192.168.1.102"] = now.Add(-30 * time.Minute) // Expired

	pm.ipViolations["192.168.1.100"] = 3
	pm.ipViolations["192.168.1.101"] = 5
	pm.ipViolations["192.168.1.102"] = 2
	pm.blacklistMutex.Unlock()

	// Run cleanup
	pm.cleanupBlacklist()

	// Check results
	pm.blacklistMutex.Lock()
	defer pm.blacklistMutex.Unlock()

	if len(pm.ipBlacklist) != 1 {
		t.Errorf("Expected 1 blacklist entry after cleanup, got %d", len(pm.ipBlacklist))
	}

	if _, exists := pm.ipBlacklist["192.168.1.101"]; !exists {
		t.Error("Valid blacklist entry should remain after cleanup")
	}

	if len(pm.ipViolations) != 1 {
		t.Errorf("Expected 1 violation entry after cleanup, got %d", len(pm.ipViolations))
	}

	if _, exists := pm.ipViolations["192.168.1.101"]; !exists {
		t.Error("Valid violation entry should remain after cleanup")
	}
}

// Test helper function to create a test server
func createTestServer(t *testing.T, pm *PairingManager, cfg config.ClientConfig) (*httptest.Server, context.CancelFunc) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pair", pm.HandlePair(cfg))
	mux.HandleFunc("/pair/confirm", pm.HandleConfirm(cfg))

	server := httptest.NewServer(mux)

	// Set up the pairing manager with the test server
	pm.SetConfig(cfg)

	_, cancel := context.WithCancel(context.Background())

	return server, cancel
}

func TestIntegrationPairingFlow(t *testing.T) {
	pm := NewPairingManager()

	cfg := config.ClientConfig{
		ClientID:                 "test-client-integration",
		VerificationCodeLength:   6,
		VerificationCodeAttempts: 3,
		PairingCodeExpiration:    1 * time.Minute,
		AllowIPSubnetMatch:       true,
	}

	// Set up temporary directories
	tmpDir, err := os.MkdirTemp("", "integration_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("MSC_STATE_PATH", tmpDir)
	os.Setenv("MSC_PAIRING_PATH", tmpDir)
	defer func() {
		os.Unsetenv("MSC_STATE_PATH")
		os.Unsetenv("MSC_PAIRING_PATH")
	}()

	server, cancel := createTestServer(t, pm, cfg)
	defer server.Close()
	defer cancel()

	// Step 1: Request pairing code
	resp, err := http.Get(server.URL + "/pair")
	if err != nil {
		t.Fatalf("Failed to request pairing code: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var pairResponse map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&pairResponse)
	if err != nil {
		t.Errorf("Failed to decode pair response: %v", err)
	}

	// Step 2: Get the generated pairing code
	code, expiry := pm.GetPairingCode()
	if code == "" {
		t.Fatal("No pairing code generated")
	}

	if len(code) != 6 {
		t.Errorf("Expected code length 6, got %d", len(code))
	}

	if time.Until(expiry) <= 0 {
		t.Error("Pairing code should not be expired")
	}

	// Step 3: Confirm pairing with correct code
	confirmRequest := map[string]string{
		"code":     code,
		"serverWs": "ws://test-server:8080/ws",
	}

	jsonBody, _ := json.Marshal(confirmRequest)
	confirmResp, err := http.Post(server.URL+"/pair/confirm", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("Failed to confirm pairing: %v", err)
	}
	defer confirmResp.Body.Close()

	if confirmResp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for confirm, got %d", confirmResp.StatusCode)
	}

	var confirmResponse map[string]interface{}
	err = json.NewDecoder(confirmResp.Body).Decode(&confirmResponse)
	if err != nil {
		t.Errorf("Failed to decode confirm response: %v", err)
	}

	if message, ok := confirmResponse["message"].(string); !ok || message != "paired" {
		t.Errorf("Expected message 'paired', got %v", message)
	}

	// Step 4: Verify state was saved
	if !state.HasState() {
		t.Error("State should be saved after successful pairing")
	}

	savedState, err := state.LoadState()
	if err != nil {
		t.Errorf("Failed to load saved state: %v", err)
	}

	if savedState.ServerWs != confirmRequest["serverWs"] {
		t.Errorf("Expected server WS %s, got %s", confirmRequest["serverWs"], savedState.ServerWs)
	}
}

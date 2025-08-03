package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"msm-client/config"
	"msm-client/state"
	"msm-client/utils"
)

// MockWebSocketServer provides a test WebSocket server
type MockWebSocketServer struct {
	server    *httptest.Server
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]bool
	mu        sync.RWMutex
	messages  []map[string]interface{}
	onMessage func(map[string]interface{})
	sessionKey string
}

// NewMockWebSocketServer creates a new mock WebSocket server
func NewMockWebSocketServer() *MockWebSocketServer {
	mock := &MockWebSocketServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for testing
			},
		},
		clients:  make(map[*websocket.Conn]bool),
		messages: make([]map[string]interface{}, 0),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", mock.handleWebSocket)
	mock.server = httptest.NewServer(mux)

	return mock
}

// GetURL returns the WebSocket URL for the mock server
func (m *MockWebSocketServer) GetURL() string {
	url := strings.Replace(m.server.URL, "http://", "ws://", 1)
	return url + "/ws"
}

// Close shuts down the mock server
func (m *MockWebSocketServer) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Close all client connections
	for conn := range m.clients {
		conn.Close()
		delete(m.clients, conn)
	}
	
	m.server.Close()
}

// SetSessionKey sets the session key for encryption/decryption
func (m *MockWebSocketServer) SetSessionKey(key string) {
	m.sessionKey = key
}

// SetOnMessage sets a callback for when messages are received
func (m *MockWebSocketServer) SetOnMessage(callback func(map[string]interface{})) {
	m.onMessage = callback
}

// GetMessages returns all received messages
func (m *MockWebSocketServer) GetMessages() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Return a copy
	messages := make([]map[string]interface{}, len(m.messages))
	copy(messages, m.messages)
	return messages
}

// ClearMessages clears the message history
func (m *MockWebSocketServer) ClearMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = m.messages[:0]
}

// SendMessage sends a message to all connected clients
func (m *MockWebSocketServer) SendMessage(message map[string]interface{}) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var messageToSend map[string]interface{}
	var err error

	// Encrypt message if session key is available
	if m.sessionKey != "" {
		messageToSend, err = utils.EncryptWebSocketMessage(message, m.sessionKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt message: %w", err)
		}
	} else {
		messageToSend = message
	}

	for conn := range m.clients {
		if err := conn.WriteJSON(messageToSend); err != nil {
			log.Printf("Error sending message to client: %v", err)
			conn.Close()
			delete(m.clients, conn)
		}
	}
	return nil
}

// handleWebSocket handles WebSocket connections
func (m *MockWebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	m.mu.Lock()
	m.clients[conn] = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.clients, conn)
		m.mu.Unlock()
		conn.Close()
	}()

	for {
		var message map[string]interface{}
		err := conn.ReadJSON(&message)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Decrypt message if session key is available
		if m.sessionKey != "" && utils.IsEncryptedWebSocketMessage(message) {
			decryptedMessage, err := utils.DecryptWebSocketMessage(message, m.sessionKey)
			if err != nil {
				log.Printf("Failed to decrypt message: %v", err)
				continue
			}
			message = decryptedMessage
		}

		// Store message
		m.mu.Lock()
		m.messages = append(m.messages, message)
		m.mu.Unlock()

		// Call callback if set
		if m.onMessage != nil {
			m.onMessage(message)
		}
	}
}

// TestEnvironment manages test setup and cleanup
type TestEnvironment struct {
	TempDir     string
	ConfigFile  string
	StateFile   string
	OriginalDir string
	MockServer  *MockWebSocketServer
	Config      config.ClientConfig
	WSManager   *WebSocketManager
}

// SetupTestEnvironment creates a temporary test environment
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
	t.Helper()

	// Set test mode environment variable
	os.Setenv("GO_TEST_MODE", "1")

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "wstest-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Store original working directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to temp directory
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create config and state directories
	configDir := filepath.Join(tempDir, "config")
	stateDir := filepath.Join(tempDir, "state")
	
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("Failed to create state directory: %v", err)
	}

	configFile := filepath.Join(configDir, "config.json")
	stateFile := filepath.Join(stateDir, "state.json")

	// Create mock server
	mockServer := NewMockWebSocketServer()

	// Create test configuration
	testConfig := config.ClientConfig{
		ClientID:             "test-client-123",
		StatusUpdateInterval: 1 * time.Second,
		DisableCommands:      true, // Disable command execution for testing
		ScreenSwitchPath:     "/usr/local/bin/ms-switch", // Mock path
	}

	// Save config to file
	configData, err := json.MarshalIndent(testConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}
	if err := os.WriteFile(configFile, configData, 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Use the test config directly instead of loading from file

	// Create WebSocket manager
	wsManager := NewWebSocketManager()
	wsManager.TestMode = true

	return &TestEnvironment{
		TempDir:     tempDir,
		ConfigFile:  configFile,
		StateFile:   stateFile,
		OriginalDir: originalDir,
		MockServer:  mockServer,
		Config:      testConfig,
		WSManager:   wsManager,
	}
}

// Cleanup cleans up the test environment
func (te *TestEnvironment) Cleanup() {
	// Close mock server
	if te.MockServer != nil {
		te.MockServer.Close()
	}

	// Shutdown WebSocket if running
	if te.WSManager != nil {
		te.WSManager.ShutdownWebSocket(false)
	}

	// Change back to original directory
	if te.OriginalDir != "" {
		os.Chdir(te.OriginalDir)
	}

	// Remove temporary directory
	if te.TempDir != "" {
		os.RemoveAll(te.TempDir)
	}

	// Clear test mode environment variable
	os.Unsetenv("GO_TEST_MODE")
}

// CreateTestState creates a test state file with session key
func (te *TestEnvironment) CreateTestState() error {
	// Set state path environment variable to use our temp directory
	os.Setenv("MSC_STATE_PATH", filepath.Dir(te.StateFile))
	
	// Generate test keys for ECDH
	if err := utils.GenerateECDHKeyPair(); err != nil {
		return fmt.Errorf("failed to generate ECDH key pair: %w", err)
	}

	clientPublicKey := utils.GetECDHPublicKey()
	
	// For testing, we'll simulate the server key exchange
	// Generate a server key pair and derive shared secret
	if err := utils.DeriveSharedSecret(clientPublicKey); err != nil {
		return fmt.Errorf("failed to derive shared secret: %w", err)
	}

	// Derive session key
	if err := utils.DeriveSessionKey("test-session"); err != nil {
		return fmt.Errorf("failed to derive session key: %w", err)
	}

	sessionKey := utils.GetSessionKey()
	
	// Set session key in mock server
	te.MockServer.SetSessionKey(sessionKey)

	// Create state using the correct structure
	testState := state.PairedState{
		ServerWs:   te.MockServer.GetURL(),
		SessionKey: sessionKey,
	}

	return state.SaveState(testState)
}

func TestNewWebSocketManager(t *testing.T) {
	wsm := NewWebSocketManager()
	
	if wsm == nil {
		t.Error("NewWebSocketManager should not return nil")
	}
	
	if wsm.IsConnected() {
		t.Error("New WebSocketManager should not be connected")
	}
	
	if wsm.IsShutdown() {
		t.Error("New WebSocketManager should not be shutdown")
	}
}

func TestWebSocketManagerBasicOperations(t *testing.T) {
	wsm := NewWebSocketManager()
	
	// Test initial state
	if wsm.GetConnection() != nil {
		t.Error("Initial connection should be nil")
	}
	
	if wsm.IsConnected() {
		t.Error("Should not be connected initially")
	}
	
	// Test shutdown operations
	wsm.SetShutdown()
	if !wsm.IsShutdown() {
		t.Error("Should be shutdown after SetShutdown")
	}
	
	wsm.ResetShutdown()
	if wsm.IsShutdown() {
		t.Error("Should not be shutdown after ResetShutdown")
	}
}

func TestWebSocketConnection(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.Cleanup()

	// Create test state
	if err := env.CreateTestState(); err != nil {
		t.Fatalf("Failed to create test state: %v", err)
	}

	// Verify state is loaded (LoadState doesn't take parameters)
	if !state.HasState() {
		t.Fatal("State should exist after creation")
	}

	// Start WebSocket connection in a goroutine
	connected := make(chan bool, 1)
	env.MockServer.SetOnMessage(func(message map[string]interface{}) {
		if msgType, ok := message["type"].(string); ok && msgType == "status" {
			connected <- true
		}
	})

	go env.WSManager.ConnectWebSocket(env.Config, env.MockServer.GetURL())

	// Wait for connection and first status message
	select {
	case <-connected:
		// Success - received status message
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for WebSocket connection")
	}

	// Verify connection state
	if !env.WSManager.IsConnected() {
		t.Error("WebSocket should be connected")
	}

	// Stop the connection
	env.WSManager.ShutdownWebSocket(true)

	// Wait a moment for shutdown
	time.Sleep(100 * time.Millisecond)

	if env.WSManager.IsConnected() {
		t.Error("WebSocket should be disconnected after shutdown")
	}
}

func TestMessageHandling(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.Cleanup()

	// Create test state
	if err := env.CreateTestState(); err != nil {
		t.Fatalf("Failed to create test state: %v", err)
	}

	// Verify state is loaded
	if !state.HasState() {
		t.Fatal("State should exist after creation")
	}

	// Set up message tracking
	receivedMessages := make([]map[string]interface{}, 0)
	messageReceived := make(chan bool, 10)
	
	env.MockServer.SetOnMessage(func(message map[string]interface{}) {
		receivedMessages = append(receivedMessages, message)
		messageReceived <- true
	})

	// Start WebSocket connection
	go env.WSManager.ConnectWebSocket(env.Config, env.MockServer.GetURL())

	// Wait for initial status message
	select {
	case <-messageReceived:
		// Connected and received first message
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial connection")
	}

	// Test ping message
	pingMessage := map[string]interface{}{
		"type":      "ping",
		"timestamp": time.Now().Unix(),
	}

	if err := env.MockServer.SendMessage(pingMessage); err != nil {
		t.Fatalf("Failed to send ping message: %v", err)
	}

	// Wait for pong response
	select {
	case <-messageReceived:
		// Should receive pong
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for pong response")
	}

	// Verify we received pong
	messages := env.MockServer.GetMessages()
	foundPong := false
	for _, msg := range messages {
		if msgType, ok := msg["type"].(string); ok && msgType == "pong" {
			foundPong = true
			break
		}
	}
	if !foundPong {
		t.Error("Should have received pong response to ping")
	}

	env.WSManager.ShutdownWebSocket(false)
}

func TestCommandHandling(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.Cleanup()

	// Enable command execution for this test
	env.Config.DisableCommands = false

	// Create test state
	if err := env.CreateTestState(); err != nil {
		t.Fatalf("Failed to create test state: %v", err)
	}

	// Verify state is loaded
	if !state.HasState() {
		t.Fatal("State should exist after creation")
	}

	// Set up message tracking
	messageReceived := make(chan bool, 10)
	
	env.MockServer.SetOnMessage(func(message map[string]interface{}) {
		messageReceived <- true
	})

	// Start WebSocket connection
	go env.WSManager.ConnectWebSocket(env.Config, env.MockServer.GetURL())

	// Wait for initial connection
	select {
	case <-messageReceived:
		// Connected
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial connection")
	}

	testCases := []struct {
		name        string
		command     map[string]interface{}
		expectError bool
	}{
		{
			name: "Status Command",
			command: map[string]interface{}{
				"type":       "command",
				"command":    "status",
				"command_id": "test-status-1",
			},
			expectError: false,
		},
		{
			name: "Reboot Command",
			command: map[string]interface{}{
				"type":       "command",
				"command":    "reboot",
				"command_id": "test-reboot-1",
			},
			expectError: false,
		},
		{
			name: "Screen List Command",
			command: map[string]interface{}{
				"type":       "command",
				"command":    "screen_list",
				"command_id": "test-list-1",
			},
			expectError: false,
		},
		{
			name: "Screen Switch Command",
			command: map[string]interface{}{
				"type":       "command",
				"command":    "screen_switch",
				"command_id": "test-switch-1",
				"params": map[string]interface{}{
					"screen_id": "2",
				},
			},
			expectError: false,
		},
		{
			name: "Screen Reload Command",
			command: map[string]interface{}{
				"type":       "command",
				"command":    "screen_reload",
				"command_id": "test-reload-1",
				"params": map[string]interface{}{
					"screen_id": "1",
				},
			},
			expectError: false,
		},
		{
			name: "Unknown Command",
			command: map[string]interface{}{
				"type":       "command",
				"command":    "unknown_command",
				"command_id": "test-unknown-1",
			},
			expectError: true,
		},
		{
			name: "Missing Command ID",
			command: map[string]interface{}{
				"type":    "command",
				"command": "status",
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env.MockServer.ClearMessages()

			// Send command
			if err := env.MockServer.SendMessage(tc.command); err != nil {
				t.Fatalf("Failed to send command: %v", err)
			}

			// Wait for response
			select {
			case <-messageReceived:
				// Received response
			case <-time.After(2 * time.Second):
				t.Fatal("Timeout waiting for command response")
			}

			// Check response
			messages := env.MockServer.GetMessages()
			if len(messages) == 0 {
				t.Fatal("No response received")
			}

			response := messages[len(messages)-1]
			responseType, ok := response["type"].(string)
			if !ok {
				t.Fatal("Response missing type field")
			}

			if tc.expectError {
				// For error cases, we expect either command_response with error status or error message
				if responseType != "command_response" && responseType != "error" {
					t.Errorf("Expected command_response or error, got: %v", responseType)
				}
			} else {
				// For success cases, we expect command_response
				if responseType != "command_response" {
					t.Errorf("Expected command_response, got: %v", responseType)
				}
			}

			// Check status
			if status, ok := response["status"].(string); ok {
				if tc.expectError {
					if status != "error" {
						t.Errorf("Expected error status for %s, got: %s", tc.name, status)
					}
				} else {
					if status == "error" {
						t.Errorf("Unexpected error status for %s: %v", tc.name, response["message"])
					}
				}
			}
		})
	}

	env.WSManager.ShutdownWebSocket(false)
}

func TestDeactivatedMessage(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.Cleanup()

	// Create test state
	if err := env.CreateTestState(); err != nil {
		t.Fatalf("Failed to create test state: %v", err)
	}

	// Verify state is loaded
	if !state.HasState() {
		t.Fatal("State should exist after creation")
	}

	// Start WebSocket connection
	connected := make(chan bool, 1)
	env.MockServer.SetOnMessage(func(message map[string]interface{}) {
		if msgType, ok := message["type"].(string); ok && msgType == "status" {
			connected <- true
		}
	})

	go env.WSManager.ConnectWebSocket(env.Config, env.MockServer.GetURL())

	// Wait for connection
	select {
	case <-connected:
		// Connected
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Verify state exists before deactivation
	if !state.HasState() {
		t.Fatal("State should exist before deactivation")
	}

	// Send deactivated message
	deactivatedMessage := map[string]interface{}{
		"type":    "deactivated",
		"message": "Device deactivated for testing",
	}

	if err := env.MockServer.SendMessage(deactivatedMessage); err != nil {
		t.Fatalf("Failed to send deactivated message: %v", err)
	}

	// Wait for connection to close and state to be deleted
	// The deactivated message handler deletes the state asynchronously
	time.Sleep(3 * time.Second)

	// Verify state was deleted (should wait until WebSocket cleanup is complete)
	maxWait := 10
	for i := 0; i < maxWait; i++ {
		if !state.HasState() {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	
	if state.HasState() {
		t.Error("State should be deleted after deactivated message")
	}
}

func TestEncryptionDecryption(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.Cleanup()

	// Create test state
	if err := env.CreateTestState(); err != nil {
		t.Fatalf("Failed to create test state: %v", err)
	}

	// Verify state is loaded
	if !state.HasState() {
		t.Fatal("State should exist after creation")
	}

	// Start WebSocket connection
	messageReceived := make(chan bool, 10)
	env.MockServer.SetOnMessage(func(message map[string]interface{}) {
		messageReceived <- true
	})

	go env.WSManager.ConnectWebSocket(env.Config, env.MockServer.GetURL())

	// Wait for initial connection
	select {
	case <-messageReceived:
		// Connected and received first message
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial connection")
	}

	// Send encrypted ping message
	pingMessage := map[string]interface{}{
		"type":      "ping",
		"timestamp": time.Now().Unix(),
	}

	if err := env.MockServer.SendMessage(pingMessage); err != nil {
		t.Fatalf("Failed to send ping message: %v", err)
	}

	// Wait for encrypted pong response
	select {
	case <-messageReceived:
		// Should receive pong
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for pong response")
	}

	// Verify we received encrypted pong
	messages := env.MockServer.GetMessages()
	foundPong := false
	for _, msg := range messages {
		if msgType, ok := msg["type"].(string); ok && msgType == "pong" {
			foundPong = true
			// Verify message has timestamp
			if _, hasTimestamp := msg["timestamp"]; !hasTimestamp {
				t.Error("Pong message should have timestamp")
			}
			break
		}
	}
	if !foundPong {
		t.Error("Should have received pong response to ping")
	}

	env.WSManager.ShutdownWebSocket(false)
}

func TestErrorHandling(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.Cleanup()

	// Create test state
	if err := env.CreateTestState(); err != nil {
		t.Fatalf("Failed to create test state: %v", err)
	}

	// Verify state is loaded
	if !state.HasState() {
		t.Fatal("State should exist after creation")
	}

	// Start WebSocket connection
	messageReceived := make(chan bool, 10)
	env.MockServer.SetOnMessage(func(message map[string]interface{}) {
		messageReceived <- true
	})

	go env.WSManager.ConnectWebSocket(env.Config, env.MockServer.GetURL())

	// Wait for initial connection
	select {
	case <-messageReceived:
		// Connected
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial connection")
	}

	// Send error message
	errorMessage := map[string]interface{}{
		"type":      "error",
		"message":   "Test error message",
		"timestamp": time.Now().Unix(),
	}

	if err := env.MockServer.SendMessage(errorMessage); err != nil {
		t.Fatalf("Failed to send error message: %v", err)
	}

	// Wait a moment for message to be processed
	time.Sleep(100 * time.Millisecond)

	// The error message should be logged but not cause disconnection
	if !env.WSManager.IsConnected() {
		t.Error("Connection should remain active after error message")
	}

	env.WSManager.ShutdownWebSocket(false)
}

// Benchmark tests
func BenchmarkWebSocketConnection(b *testing.B) {
	// Set up temporary directory for state
	tmpDir := b.TempDir()
	oldPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tmpDir)
	defer os.Setenv("MSC_STATE_PATH", oldPath)

	// Create test state directly for benchmark (without mock server)
	testState := state.PairedState{
		ServerWs:   "ws://localhost:8080/ws",
		SessionKey: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=", // Base64 encoded 32-byte key
	}

	if err := state.SaveState(testState); err != nil {
		b.Fatalf("Failed to save test state: %v", err)
	}

	// Verify state is loaded
	if !state.HasState() {
		b.Fatal("State should exist after creation")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Note: This is a simplified benchmark
		// In practice, you'd want to test specific operations
		wsm := NewWebSocketManager()
		wsm.TestMode = true
		
		// Test basic operations
		wsm.SetShutdown()
		wsm.ResetShutdown()
		wsm.IsConnected()
		wsm.IsShutdown()
	}
}

func BenchmarkMessageProcessing(b *testing.B) {
	// Set up temporary directory for state
	tmpDir := b.TempDir()
	oldPath := os.Getenv("MSC_STATE_PATH")
	os.Setenv("MSC_STATE_PATH", tmpDir)
	defer os.Setenv("MSC_STATE_PATH", oldPath)

	// Create test state directly for benchmark (without mock server)
	testState := state.PairedState{
		ServerWs:   "ws://localhost:8080/ws",
		SessionKey: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=", // Base64 encoded 32-byte key
	}

	if err := state.SaveState(testState); err != nil {
		b.Fatalf("Failed to save test state: %v", err)
	}

	// Verify state is loaded
	if !state.HasState() {
		b.Fatal("State should exist after creation")
	}

	// Create test message
	testMessage := map[string]interface{}{
		"type":       "command",
		"command":    "status",
		"command_id": "bench-test",
		"timestamp":  time.Now().Unix(),
	}

	sessionKey := state.GetSessionKey()
	encryptedMessage, err := utils.EncryptWebSocketMessage(testMessage, sessionKey)
	if err != nil {
		b.Fatalf("Failed to encrypt test message: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Test message encryption/decryption
		if utils.IsEncryptedWebSocketMessage(encryptedMessage) {
			_, err := utils.DecryptWebSocketMessage(encryptedMessage, sessionKey)
			if err != nil {
				b.Errorf("Failed to decrypt message: %v", err)
			}
		}
	}
}

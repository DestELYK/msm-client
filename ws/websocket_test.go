package ws

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"msm-client/config"
	"msm-client/state"

	"github.com/gorilla/websocket"
)

// Enable test mode for all tests
func init() {
	TestMode = true
}

// Helper function to set up temporary directories for testing
func setupTestPaths(t *testing.T) (cleanup func()) {
	tempDir := t.TempDir()

	// Store original environment variables
	originalStatePath := os.Getenv("MSC_STATE_PATH")
	originalConfigPath := os.Getenv("MSC_CONFIG_PATH")
	originalPairingPath := os.Getenv("MSC_PAIRING_PATH")

	// Set environment variables to use temp directory
	os.Setenv("MSC_STATE_PATH", tempDir)
	os.Setenv("MSC_CONFIG_PATH", tempDir)
	os.Setenv("MSC_PAIRING_PATH", tempDir)

	return func() {
		// Restore original environment variables
		os.Setenv("MSC_STATE_PATH", originalStatePath)
		os.Setenv("MSC_CONFIG_PATH", originalConfigPath)
		os.Setenv("MSC_PAIRING_PATH", originalPairingPath)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for testing
	},
}

// MockWebSocketServer represents a test WebSocket server that mimics the real server
type MockWebSocketServer struct {
	server         *httptest.Server
	connections    map[*websocket.Conn]bool
	messages       []map[string]interface{}
	mu             sync.Mutex
	messageHandler func(*websocket.Conn, map[string]interface{})
}

// NewMockWebSocketServer creates a new mock WebSocket server
func NewMockWebSocketServer() *MockWebSocketServer {
	mock := &MockWebSocketServer{
		connections: make(map[*websocket.Conn]bool),
		messages:    make([]map[string]interface{}, 0),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", mock.handleWebSocket)

	mock.server = httptest.NewServer(mux)

	return mock
}

func (m *MockWebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check for authorization header
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	m.mu.Lock()
	m.connections[conn] = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.connections, conn)
		m.mu.Unlock()
	}()

	for {
		var message map[string]interface{}
		err := conn.ReadJSON(&message)
		if err != nil {
			break
		}

		m.mu.Lock()
		m.messages = append(m.messages, message)
		m.mu.Unlock()

		if m.messageHandler != nil {
			m.messageHandler(conn, message)
		}
	}
}

func (m *MockWebSocketServer) SetMessageHandler(handler func(*websocket.Conn, map[string]interface{})) {
	m.messageHandler = handler
}

func (m *MockWebSocketServer) GetURL() string {
	return strings.Replace(m.server.URL, "http://", "ws://", 1) + "/ws"
}

func (m *MockWebSocketServer) GetMessages() []map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]map[string]interface{}, len(m.messages))
	copy(result, m.messages)
	return result
}

func (m *MockWebSocketServer) SendMessage(conn *websocket.Conn, message map[string]interface{}) error {
	return conn.WriteJSON(message)
}

func (m *MockWebSocketServer) GetConnectionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.connections)
}

func (m *MockWebSocketServer) Close() {
	m.server.Close()
}

// TestConnectWebSocket tests the actual ConnectWebSocket function
func TestConnectWebSocket(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	// Create test config and state
	cfg := config.ClientConfig{
		ClientID:       "test-client-123",
		UpdateInterval: 30,
	}

	testState := state.PairedState{
		ServerWs: mockServer.GetURL(),
		Token:    "test-token-abc",
	}

	err := state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save test state: %v", err)
	}

	// Set up message handler to capture status messages
	statusReceived := make(chan bool, 1)
	mockServer.SetMessageHandler(func(conn *websocket.Conn, message map[string]interface{}) {
		if msgType, ok := message["type"].(string); ok && msgType == "status" {
			if clientID, ok := message["clientId"].(string); ok && clientID == cfg.ClientID {
				statusReceived <- true
			}
		}
	})

	// Start ConnectWebSocket in a goroutine since it runs indefinitely
	done := make(chan bool)
	go func() {
		ConnectWebSocket(cfg, mockServer.GetURL(), testState.Token)
		done <- true
	}()

	// Wait for status message or timeout
	select {
	case <-statusReceived:
		// Success - received status message from ConnectWebSocket
	case <-time.After(15 * time.Second):
		t.Fatal("ConnectWebSocket test timed out waiting for status message")
	}

	// Delete state to stop the connection
	err = state.DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Wait for ConnectWebSocket to exit
	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("ConnectWebSocket did not exit after state deletion")
	}
}

// TestHandleMessage tests the handleMessage function with ping messages
func TestHandleMessage(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	// Test ping message handling
	pongReceived := make(chan bool, 1)
	mockServer.SetMessageHandler(func(conn *websocket.Conn, message map[string]interface{}) {
		// When we receive a pong response, mark test as successful
		if msgType, ok := message["type"].(string); ok && msgType == "pong" {
			pongReceived <- true
		}
	})

	// Create a direct connection to test handleMessage function
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer test-token")

	conn, _, err := websocket.DefaultDialer.Dial(mockServer.GetURL(), headers)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Test ping message
	pingMessage := map[string]interface{}{
		"type":      "ping",
		"timestamp": time.Now().Unix(),
	}

	// Simulate receiving and handling the ping message
	handleMessage(conn, pingMessage)

	// Wait for pong response
	select {
	case <-pongReceived:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("handleMessage ping test timed out waiting for pong")
	}
}

// TestHandleCommand tests the handleCommand function with different commands
func TestHandleCommand(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	tests := []struct {
		name            string
		command         CommandType
		expectedStatus  ResponseStatus
		expectedMessage string
	}{
		{
			name:            "Status Command",
			command:         CommandStatus,
			expectedStatus:  StatusSuccess,
			expectedMessage: "",
		},
		{
			name:            "Reboot Command",
			command:         CommandReboot,
			expectedStatus:  StatusAcknowledged,
			expectedMessage: "Reboot command received, system would reboot",
		},
		{
			name:            "Unknown Command",
			command:         CommandType("unknown"),
			expectedStatus:  StatusError,
			expectedMessage: "Unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responseReceived := make(chan map[string]interface{}, 1)

			mockServer.SetMessageHandler(func(conn *websocket.Conn, message map[string]interface{}) {
				if msgType, ok := message["type"].(string); ok && msgType == "command_response" {
					responseReceived <- message
				}
			})

			// Create connection
			headers := make(http.Header)
			headers.Set("Authorization", "Bearer test-token")

			conn, _, err := websocket.DefaultDialer.Dial(mockServer.GetURL(), headers)
			if err != nil {
				t.Fatalf("Failed to connect to WebSocket: %v", err)
			}
			defer conn.Close()

			// Test command message
			commandMessage := map[string]interface{}{
				"type":    "command",
				"command": string(tt.command),
			}

			// Handle the command message
			handleCommand(conn, commandMessage)

			// Wait for response
			select {
			case response := <-responseReceived:
				if status, ok := response["status"].(string); ok {
					if ResponseStatus(status) != tt.expectedStatus {
						t.Fatalf("Expected status %s, got %s", tt.expectedStatus, status)
					}
				} else {
					t.Fatal("Response missing status field")
				}

				if tt.expectedMessage != "" {
					if message, ok := response["message"].(string); ok {
						if message != tt.expectedMessage {
							t.Fatalf("Expected message '%s', got '%s'", tt.expectedMessage, message)
						}
					} else {
						t.Fatal("Response missing message field")
					}
				}

			case <-time.After(2 * time.Second):
				t.Fatalf("handleCommand %s test timed out", tt.name)
			}
		})
	}
}

// TestHandleCommandDisabled tests the handleCommand function when commands are disabled
func TestHandleCommandDisabled(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	// Set up config with commands disabled
	cfg := config.ClientConfig{
		ClientID:        "test-client-disabled",
		UpdateInterval:  30,
		DisableCommands: true,
	}

	// Store the config globally (simulate what ConnectWebSocket does)
	mu.Lock()
	clientConfig = cfg
	mu.Unlock()

	tests := []struct {
		name            string
		command         CommandType
		expectedStatus  ResponseStatus
		expectedMessage string
	}{
		{
			name:            "Reboot Command Disabled",
			command:         CommandReboot,
			expectedStatus:  StatusError,
			expectedMessage: "Command execution is disabled on this client",
		},
		{
			name:            "Status Command Disabled",
			command:         CommandStatus,
			expectedStatus:  StatusError,
			expectedMessage: "Command execution is disabled on this client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responseReceived := make(chan map[string]interface{}, 1)

			mockServer.SetMessageHandler(func(conn *websocket.Conn, message map[string]interface{}) {
				if msgType, ok := message["type"].(string); ok && msgType == "command_response" {
					responseReceived <- message
				}
			})

			// Create connection
			headers := make(http.Header)
			headers.Set("Authorization", "Bearer test-token")

			conn, _, err := websocket.DefaultDialer.Dial(mockServer.GetURL(), headers)
			if err != nil {
				t.Fatalf("Failed to connect to WebSocket: %v", err)
			}
			defer conn.Close()

			// Test command message
			commandMessage := map[string]interface{}{
				"type":    "command",
				"command": string(tt.command),
			}

			// Handle the command message
			handleCommand(conn, commandMessage)

			// Wait for response
			select {
			case response := <-responseReceived:
				if status, ok := response["status"].(string); ok {
					if ResponseStatus(status) != tt.expectedStatus {
						t.Fatalf("Expected status %s, got %s", tt.expectedStatus, status)
					}
				} else {
					t.Fatal("Response missing status field")
				}

				if tt.expectedMessage != "" {
					if message, ok := response["message"].(string); ok {
						if message != tt.expectedMessage {
							t.Fatalf("Expected message '%s', got '%s'", tt.expectedMessage, message)
						}
					} else {
						t.Fatal("Response missing message field")
					}
				}

			case <-time.After(2 * time.Second):
				t.Fatalf("handleCommand %s test timed out", tt.name)
			}
		})
	}

	// Reset the global config for other tests
	mu.Lock()
	clientConfig = config.ClientConfig{}
	mu.Unlock()
}

// TestHandleCommandEnabled tests the handleCommand function when commands are enabled (default)
func TestHandleCommandEnabled(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	// Set up config with commands enabled (default)
	cfg := config.ClientConfig{
		ClientID:        "test-client-enabled",
		UpdateInterval:  30,
		DisableCommands: false, // Explicitly set to false, though this is the default
	}

	// Store the config globally (simulate what ConnectWebSocket does)
	mu.Lock()
	clientConfig = cfg
	mu.Unlock()

	responseReceived := make(chan map[string]interface{}, 1)

	mockServer.SetMessageHandler(func(conn *websocket.Conn, message map[string]interface{}) {
		if msgType, ok := message["type"].(string); ok && msgType == "command_response" {
			responseReceived <- message
		}
	})

	// Create connection
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer test-token")

	conn, _, err := websocket.DefaultDialer.Dial(mockServer.GetURL(), headers)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Test status command (should work when commands are enabled)
	commandMessage := map[string]interface{}{
		"type":    "command",
		"command": "status",
	}

	// Handle the command message
	handleCommand(conn, commandMessage)

	// Wait for response
	select {
	case response := <-responseReceived:
		if status, ok := response["status"].(string); ok {
			if ResponseStatus(status) != StatusSuccess {
				t.Fatalf("Expected status %s, got %s", StatusSuccess, status)
			}
		} else {
			t.Fatal("Response missing status field")
		}

		if command, ok := response["command"].(string); ok {
			if command != "status" {
				t.Fatalf("Expected command 'status', got '%s'", command)
			}
		} else {
			t.Fatal("Response missing command field")
		}

	case <-time.After(2 * time.Second):
		t.Fatal("handleCommand enabled test timed out")
	}

	// Reset the global config for other tests
	mu.Lock()
	clientConfig = config.ClientConfig{}
	mu.Unlock()
}

// TestConnectionFailure tests connection failure scenarios
func TestConnectionFailure(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	// Test connection to non-existent server
	cfg := config.ClientConfig{
		ClientID:       "test-client-123",
		UpdateInterval: 30,
	}

	// Create state with invalid server URL
	testState := state.PairedState{
		ServerWs: "ws://localhost:99999/ws",
		Token:    "test-token",
	}

	err := state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save test state: %v", err)
	}

	// Start ConnectWebSocket in a goroutine
	done := make(chan bool)
	go func() {
		// This should fail to connect and keep retrying until state is deleted
		ConnectWebSocket(cfg, testState.ServerWs, testState.Token)
		done <- true
	}()

	// Wait a bit for connection attempts
	time.Sleep(2 * time.Second)

	// Delete state to stop connection attempts
	err = state.DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Wait for ConnectWebSocket to exit
	select {
	case <-done:
		// Success - ConnectWebSocket should exit when state is deleted
	case <-time.After(5 * time.Second):
		t.Fatal("ConnectWebSocket did not exit after state deletion during connection failure")
	}
}

// TestSendResponse tests the sendResponse function
func TestSendResponse(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	responseReceived := make(chan map[string]interface{}, 1)

	mockServer.SetMessageHandler(func(conn *websocket.Conn, message map[string]interface{}) {
		responseReceived <- message
	})

	// Create connection
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer test-token")

	conn, _, err := websocket.DefaultDialer.Dial(mockServer.GetURL(), headers)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Test sendResponse function
	testData := map[string]interface{}{
		"command": "status",
		"status":  "success",
		"uptime":  12345,
	}

	sendResponse(conn, MessageTypeCommandResponse, testData)

	// Wait for response
	select {
	case response := <-responseReceived:
		if msgType, ok := response["type"].(string); !ok || msgType != "command_response" {
			t.Fatalf("Expected type 'command_response', got %v", response["type"])
		}

		if command, ok := response["command"].(string); !ok || command != "status" {
			t.Fatalf("Expected command 'status', got %v", response["command"])
		}

		if status, ok := response["status"].(string); !ok || status != "success" {
			t.Fatalf("Expected status 'success', got %v", response["status"])
		}

	case <-time.After(2 * time.Second):
		t.Fatal("sendResponse test timed out")
	}
}

// TestStateMonitoring tests the state file monitoring functionality
func TestStateMonitoring(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	// Create test config and state
	cfg := config.ClientConfig{
		ClientID:       "test-client-123",
		UpdateInterval: 30,
	}

	testState := state.PairedState{
		ServerWs: mockServer.GetURL(),
		Token:    "test-token",
	}

	err := state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save test state: %v", err)
	}

	// Start ConnectWebSocket in a goroutine
	done := make(chan bool)
	go func() {
		ConnectWebSocket(cfg, mockServer.GetURL(), testState.Token)
		done <- true
	}()

	// Wait for connection to establish
	time.Sleep(2 * time.Second)

	// Verify state exists
	if !state.HasState() {
		t.Fatal("State should exist during connection")
	}

	// Delete state to trigger disconnection
	err = state.DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Wait for ConnectWebSocket to detect state deletion and exit
	select {
	case <-done:
		// Success - ConnectWebSocket should exit when state is deleted
	case <-time.After(10 * time.Second):
		t.Fatal("ConnectWebSocket did not exit after state deletion")
	}

	// Verify state was deleted
	if state.HasState() {
		t.Fatal("State should be deleted after monitoring detected deletion")
	}
}

// TestReconnectionLogic tests the reconnection logic in ConnectWebSocket
func TestReconnectionLogic(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	// Create config and state
	cfg := config.ClientConfig{
		ClientID:       "test-client-123",
		UpdateInterval: 30,
	}

	// Test with server that starts down, then comes up
	mockServer := NewMockWebSocketServer()

	testState := state.PairedState{
		ServerWs: mockServer.GetURL(),
		Token:    "test-token",
	}

	err := state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save test state: %v", err)
	}

	// Close server to simulate it being down initially
	mockServer.Close()

	// Start ConnectWebSocket in a goroutine
	done := make(chan bool)
	go func() {
		ConnectWebSocket(cfg, testState.ServerWs, testState.Token)
		done <- true
	}()

	// Wait for initial connection attempts to fail
	time.Sleep(3 * time.Second)

	// Start a new server (simulating server coming back online)
	newMockServer := NewMockWebSocketServer()
	defer newMockServer.Close()

	// Update state with new server URL
	testState.ServerWs = newMockServer.GetURL()
	err = state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to update test state: %v", err)
	}

	// Wait a bit more for connection attempts
	time.Sleep(2 * time.Second)

	// Delete state to stop reconnection attempts
	err = state.DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Wait for ConnectWebSocket to exit
	select {
	case <-done:
		// Success - ConnectWebSocket should exit when state is deleted
	case <-time.After(5 * time.Second):
		t.Fatal("ConnectWebSocket did not exit after state deletion during reconnection test")
	}
}

// TestGlobalConnection tests the global connection functionality
func TestGlobalConnection(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	defer os.Remove("paired.json")

	// Initially no connection should exist
	if IsConnected() {
		t.Fatal("Expected no connection initially")
	}

	if GetConnection() != nil {
		t.Fatal("Expected nil connection initially")
	}

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	// Create test config and state
	cfg := config.ClientConfig{
		ClientID:       "test-client-123",
		UpdateInterval: 30,
	}

	testState := state.PairedState{
		ServerWs: mockServer.GetURL(),
		Token:    "test-token",
	}

	err := state.SaveState(testState)
	if err != nil {
		t.Fatalf("Failed to save test state: %v", err)
	}

	// Start ConnectWebSocket in a goroutine
	connected := make(chan bool, 1)
	done := make(chan bool)

	go func() {
		ConnectWebSocket(cfg, mockServer.GetURL(), testState.Token)
		done <- true
	}()

	// Wait for connection to be established
	go func() {
		for i := 0; i < 50; i++ { // Check for up to 5 seconds
			if IsConnected() {
				connected <- true
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-connected:
		// Verify global connection is available
		if !IsConnected() {
			t.Fatal("Expected connection to be established")
		}

		if GetConnection() == nil {
			t.Fatal("Expected non-nil global connection")
		}

		// Test SendMessage function
		err = SendMessage(MessageTypeStatus, map[string]interface{}{
			"clientId": cfg.ClientID,
			"uptime":   time.Now().Unix(),
		})
		if err != nil {
			t.Fatalf("Failed to send message via global connection: %v", err)
		}

	case <-time.After(10 * time.Second):
		t.Fatal("Connection was not established within timeout")
	}

	// Clean up
	err = state.DeleteState()
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Wait for disconnection
	select {
	case <-done:
		// Verify connection is cleared
		if IsConnected() {
			t.Fatal("Expected connection to be cleared after state deletion")
		}

		if GetConnection() != nil {
			t.Fatal("Expected nil connection after state deletion")
		}

	case <-time.After(10 * time.Second):
		t.Fatal("ConnectWebSocket did not exit after state deletion")
	}
}

func TestDisconnectMessage(t *testing.T) {
	// Create mock server that captures disconnect messages
	var disconnectMessageReceived bool
	var disconnectMessage map[string]interface{}

	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	// Set custom message handler to capture disconnect messages
	mockServer.SetMessageHandler(func(conn *websocket.Conn, msg map[string]interface{}) {
		if msgType, ok := msg["type"].(string); ok && msgType == "disconnect" {
			disconnectMessageReceived = true
			disconnectMessage = msg
		}
	})

	// Create connection manually for testing
	wsURL := mockServer.GetURL()

	// Create request headers with Authorization token
	requestHeaders := http.Header{}
	requestHeaders.Add("Authorization", "Bearer test-token")

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, requestHeaders)
	if err != nil {
		t.Fatalf("Failed to connect to mock server: %v", err)
	}

	// Set the global connection with headers from response
	headers := make(http.Header)
	if resp != nil && resp.Header != nil {
		headers = resp.Header
	}
	setConnection(conn, headers)

	// Wait a bit for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Call DisconnectWebSocket
	err = DisconnectWebSocket(conn)
	if err != nil {
		t.Fatalf("DisconnectWebSocket failed: %v", err)
	}

	// Wait a bit for message to be processed
	time.Sleep(200 * time.Millisecond)

	// Verify disconnect message was sent
	if !disconnectMessageReceived {
		t.Fatal("Expected disconnect message to be sent to server")
	}

	// Verify message content
	if disconnectMessage["type"] != "disconnect" {
		t.Fatalf("Expected message type 'disconnect', got %v", disconnectMessage["type"])
	}

	if disconnectMessage["message"] != "client_disconnecting" {
		t.Fatalf("Expected message 'client_disconnecting', got %v", disconnectMessage["message"])
	}

	if _, ok := disconnectMessage["timestamp"]; !ok {
		t.Fatal("Expected timestamp field in disconnect message")
	}

	// Verify connection is cleared
	if IsConnected() {
		t.Fatal("Expected connection to be cleared after disconnect")
	}

	if GetConnection() != nil {
		t.Fatal("Expected nil connection after disconnect")
	}
}

func TestHandleDeactivatedMessage(t *testing.T) {
	// Set up temporary paths for testing
	cleanup := setupTestPaths(t)
	defer cleanup()

	// Create a temporary state file for testing
	tempState := state.PairedState{
		ServerWs: "ws://test.com/ws",
		Token:    "test-token",
	}

	err := state.SaveState(tempState)
	if err != nil {
		t.Fatalf("Failed to save test state: %v", err)
	}

	// Ensure cleanup
	defer func() {
		if state.HasState() {
			state.DeleteState()
		}
	}()

	// Verify state exists before test
	if !state.HasState() {
		t.Fatal("State file should exist before deactivated message")
	}

	// Create mock server
	mockServer := NewMockWebSocketServer()
	defer mockServer.Close()

	// Start client connection in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		cfg := config.ClientConfig{
			ClientID: "test-client",
		}

		// Replace the server URL with our mock server
		serverURL := mockServer.GetURL()
		ConnectWebSocket(cfg, serverURL, "test-token")
	}()

	// Wait for connection to be established
	time.Sleep(200 * time.Millisecond)

	// Send deactivated message from server
	mockServer.mu.Lock()
	for conn := range mockServer.connections {
		deactivatedMsg := map[string]interface{}{
			"type":    "deactivated",
			"message": "Device not found in active device list. Please contact administrator.",
		}
		conn.WriteJSON(deactivatedMsg)
		break
	}
	mockServer.mu.Unlock()

	// Wait for message to be processed and connection to close
	time.Sleep(500 * time.Millisecond)

	// Verify state file was deleted
	if state.HasState() {
		t.Fatal("State file should be deleted after deactivated message")
	}

	// Verify connection is cleared
	if IsConnected() {
		t.Fatal("Expected connection to be cleared after deactivated message")
	}

	if GetConnection() != nil {
		t.Fatal("Expected nil connection after deactivated message")
	}

	// Wait for goroutine to finish
	wg.Wait()
}

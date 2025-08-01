package ws

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"msm-client/config"
	"msm-client/state"
	"msm-client/utils"
)

// Global WebSocket connection variables
type WebSocketManager struct {
	Connection *websocket.Conn
	Headers    http.Header
	mu         sync.RWMutex
	connected  bool
	shutdown   bool // Flag to prevent reconnection during shutdown
	// TestMode prevents actual command execution during testing
	TestMode bool
	// Current client configuration
	clientConfig config.ClientConfig
}

// MessageType represents the type of WebSocket message
type MessageType string

const (
	// Incoming message types
	MessageTypePing        MessageType = "ping"
	MessageTypeCommand     MessageType = "command"
	MessageTypeDeactivated MessageType = "deactivated"

	// Outgoing message types
	MessageTypePong            MessageType = "pong"
	MessageTypeStatus          MessageType = "status"
	MessageTypeCommandResponse MessageType = "command_response"
	MessageTypeError           MessageType = "error"
	MessageTypeDisconnect      MessageType = "disconnect"
)

// CommandType represents the type of command
type CommandType string

const (
	CommandReboot CommandType = "reboot"
	CommandStatus CommandType = "status"
)

// ResponseStatus represents the status of a command response
type ResponseStatus string

const (
	StatusAcknowledged ResponseStatus = "acknowledged"
	StatusSuccess      ResponseStatus = "success"
	StatusError        ResponseStatus = "error"
)

// NewWebSocketManager creates a new WebSocketManager instance
func NewWebSocketManager() *WebSocketManager {
	return &WebSocketManager{
		TestMode: isTestEnvironment(),
	}
}

// generateStatusData creates a status data map with current client information
func (wsm *WebSocketManager) generateStatusData() map[string]any {
	wsm.mu.RLock()
	clientID := wsm.clientConfig.ClientID
	wsm.mu.RUnlock()

	return map[string]any{
		"clientId":   clientID,
		"uptime":     utils.GetUptime(),
		"interfaces": utils.GetNetworkInterfaces(),
		"timestamp":  time.Now().Format(time.RFC3339),
	}
}

// isTestEnvironment checks if we're running in a test environment
func isTestEnvironment() bool {
	return os.Getenv("GO_TEST_MODE") == "1"
}

// GetConnection returns the current WebSocket connection (thread-safe)
func (wsm *WebSocketManager) GetConnection() *websocket.Conn {
	wsm.mu.RLock()
	defer wsm.mu.RUnlock()
	return wsm.Connection
}

// IsConnected returns whether the WebSocket is currently connected (thread-safe)
func (wsm *WebSocketManager) IsConnected() bool {
	wsm.mu.RLock()
	defer wsm.mu.RUnlock()
	return wsm.connected && wsm.Connection != nil
}

// SetShutdown sets the shutdown flag to prevent reconnection (thread-safe)
func (wsm *WebSocketManager) SetShutdown() {
	wsm.mu.Lock()
	defer wsm.mu.Unlock()
	wsm.shutdown = true
}

// IsShutdown returns whether shutdown has been initiated (thread-safe)
func (wsm *WebSocketManager) IsShutdown() bool {
	wsm.mu.RLock()
	defer wsm.mu.RUnlock()
	return wsm.shutdown
}

// ResetShutdown clears the shutdown flag to allow reconnection (thread-safe)
func (wsm *WebSocketManager) ResetShutdown() {
	wsm.mu.Lock()
	defer wsm.mu.Unlock()
	wsm.shutdown = false
}

// setConnection sets the global connection and headers (thread-safe)
func (wsm *WebSocketManager) setConnection(conn *websocket.Conn, headers http.Header) {
	wsm.mu.Lock()
	defer wsm.mu.Unlock()
	wsm.Connection = conn
	wsm.Headers = headers
	wsm.connected = true
}

// clearConnection clears the global connection and headers (thread-safe)
func (wsm *WebSocketManager) clearConnection() {
	wsm.mu.Lock()
	defer wsm.mu.Unlock()
	if wsm.Connection != nil {
		wsm.Connection.Close()
	}
	wsm.Connection = nil
	wsm.Headers = nil
	wsm.connected = false
}

// SendMessage sends a message using the global connection (thread-safe)
func (wsm *WebSocketManager) SendMessage(messageType MessageType, data map[string]interface{}) error {
	conn := wsm.GetConnection()
	if conn == nil {
		return websocket.ErrCloseSent // Connection not available
	}

	return wsm.sendResponse(conn, messageType, data)
}

func (wsm *WebSocketManager) ConnectWebSocket(cfg config.ClientConfig, serverWs string) {
	// Store config globally for use in command handling
	wsm.mu.Lock()
	wsm.clientConfig = cfg
	wsm.mu.Unlock()

	// Parse WebSocket URL and add client_id as query parameter
	wsURL, err := url.Parse(serverWs)
	if err != nil {
		log.Printf("Failed to parse WebSocket URL: %v", err)
		return
	}

	// Add client_id query parameter
	query := wsURL.Query()
	query.Set("client_id", cfg.ClientID)
	wsURL.RawQuery = query.Encode()

	headers := make(http.Header)

	backoff := time.Second
	for {
		// Check if shutdown has been initiated
		if wsm.IsShutdown() {
			log.Println("Shutdown initiated, stopping WebSocket connection attempts")
			return
		}

		// Check if state file still exists before attempting connection
		if !state.HasState() {
			log.Println("State file no longer exists, stopping WebSocket connection")
			return
		}

		c, _, err := websocket.DefaultDialer.Dial(wsURL.String(), headers)
		if err != nil {
			log.Printf("WebSocket connection failed: %v (retrying in %s)", err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			continue
		}

		log.Printf("Connected to %s", serverWs)
		backoff = time.Second

		// Set global connection variables
		wsm.setConnection(c, headers)

		// Channel to signal when connection should close
		done := make(chan struct{})
		stateDeleted := make(chan struct{})
		deactivated := make(chan struct{})
		var closeOnce sync.Once

		// Goroutine to listen for incoming messages
		go func() {
			defer closeOnce.Do(func() { close(done) })
			for {
				if wsm.IsShutdown() {
					return
				}

				var message map[string]interface{}
				err := c.ReadJSON(&message)
				if err != nil {
					log.Printf("Read failed: %v", err)
					return
				}

				// Check if this is a deactivated message
				if msgType, ok := message["type"].(string); ok && MessageType(msgType) == MessageTypeDeactivated {
					wsm.handleDeactivated(c, message)
					closeOnce.Do(func() { close(deactivated) })
					return
				}

				// Handle other incoming messages
				wsm.handleMessage(c, message)
			}
		}()

		// Goroutine to send periodic status updates
		go func() {
			// Use shorter interval in test mode for faster test execution
			interval := 10 * time.Second
			if isTestEnvironment() {
				interval = 1 * time.Second
			}

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if wsm.IsShutdown() {
						closeOnce.Do(func() { close(done) })
						return
					}

					statusData := wsm.generateStatusData()

					err := wsm.sendResponse(c, MessageTypeStatus, statusData)
					if err != nil {
						log.Printf("Write failed: %v", err)
						return
					}
				case <-done:
					return
				case <-stateDeleted:
					return
				case <-deactivated:
					return
				}
			}
		}()

		// Goroutine to check if state file still exists
		go func() {
			// Use shorter interval in test mode for faster test execution
			interval := 5 * time.Second
			if isTestEnvironment() {
				interval = 500 * time.Millisecond
			}

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if wsm.IsShutdown() {
						closeOnce.Do(func() { close(done) })
						return
					}

					if !state.HasState() {
						log.Println("State file no longer exists, closing WebSocket connection to restart pairing")
						closeOnce.Do(func() { close(stateDeleted) })
						return
					}
				case <-done:
					return
				case <-stateDeleted:
					return
				case <-deactivated:
					return
				}
			}
		}()

		// Wait for either goroutine to finish
		select {
		case <-done:
			wsm.clearConnection()
			// Check if shutdown has been initiated before attempting reconnect
			if wsm.IsShutdown() {
				log.Println("WebSocket connection closed during shutdown, not reconnecting")
				return
			}
			log.Println("WebSocket connection closed, attempting to reconnect...")
		case <-stateDeleted:
			wsm.clearConnection()
			log.Println("State file deleted, closing WebSocket to restart pairing server")
			return // Exit function to allow pairing server restart
		case <-deactivated:
			wsm.clearConnection()
			log.Println("Device deactivated by server, exiting WebSocket connection")
			return // Exit function to stop WebSocket and allow pairing restart
		}
	}
}

func (wsm *WebSocketManager) handleMessage(c *websocket.Conn, message map[string]interface{}) {
	// Check if message is encrypted and decrypt if necessary
	if utils.IsEncryptedWebSocketMessage(message) {
		sessionKey := state.GetSessionKey()
		if sessionKey != "" {
			decryptedMessage, err := utils.DecryptWebSocketMessage(message, sessionKey)
			if err != nil {
				log.Printf("Failed to decrypt message: %v.", err)
				wsm.ShutdownWebSocket(false)
				state.DeleteState() // Clear state on decryption failure
				return
			}
			message = decryptedMessage
		} else {
			log.Printf("Received encrypted message but no session key available")
			wsm.sendResponse(c, MessageTypeError, map[string]interface{}{
				"message":   "No session key available for decryption",
				"timestamp": time.Now().Unix(),
			})
			return
		}
	} else {
		log.Println("Received unencrypted message, disconnecting from server")
		wsm.ShutdownWebSocket(false)
		state.DeleteState() // Clear state on decryption failure
		return
	}

	msgType, ok := message["type"].(string)
	if !ok {
		log.Printf("Received message without type: %v", message)
		return
	}

	switch MessageType(msgType) {
	case MessageTypePing:
		log.Println("Received ping from server")
		// Respond to ping with pong
		wsm.sendResponse(c, MessageTypePong, map[string]interface{}{
			"timestamp": time.Now().Unix(),
		})
	case MessageTypeCommand:
		wsm.handleCommand(c, message)
	case MessageTypeDeactivated:
		wsm.handleDeactivated(c, message)
	case MessageTypeError:
		wsm.handleError(c, message)
	default:
		log.Printf("Received unknown message type '%s': %v", msgType, message)
	}
}

func (wsm *WebSocketManager) handleCommand(c *websocket.Conn, message map[string]interface{}) {
	command, ok := message["command"].(string)
	if !ok {
		log.Printf("Command message missing 'command' field: %v", message)
		wsm.sendResponse(c, MessageTypeError, map[string]interface{}{
			"message": "Command field missing",
		})
		return
	}

	// Extract command_id if present
	commandID, hasCommandID := message["command_id"].(string)
	if !hasCommandID {
		log.Printf("Command message missing 'command_id' field: %v", message)
		wsm.sendResponse(c, MessageTypeError, map[string]interface{}{
			"message": "Command ID field missing",
		})
		return
	}

	log.Printf("Received command: %s (ID: %s)", command, commandID)

	// Check if commands are disabled
	wsm.mu.RLock()
	commandsDisabled := wsm.clientConfig.DisableCommands
	wsm.mu.RUnlock()

	if commandsDisabled {
		log.Printf("Command execution disabled, rejecting command: %s", command)
		wsm.sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
			"command":    command,
			"command_id": commandID,
			"status":     StatusError,
			"message":    "Command execution is disabled on this client",
		})
		return
	}

	switch CommandType(command) {
	case CommandReboot:
		log.Println("Reboot command received - would reboot system")
		wsm.sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
			"command":    CommandReboot,
			"command_id": commandID,
			"status":     StatusAcknowledged,
			"message":    "Reboot command received, system would reboot",
		})

		// Only execute actual reboot command if not in test environment
		if !isTestEnvironment() {
			cmd := exec.Command("reboot")
			err := cmd.Run()
			if err != nil {
				log.Printf("Failed to execute reboot command: %v", err)
				wsm.sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
					"command":    CommandReboot,
					"command_id": commandID,
					"status":     StatusError,
					"message":    "Failed to execute reboot command",
				})
				return
			}
		} else {
			log.Println("Test mode: Reboot command acknowledged but not executed")
		}
	case CommandStatus:
		log.Println("Status request received")

		// Generate status data
		statusData := make(map[string]interface{})
		statusData["data"] = wsm.generateStatusData()
		statusData["command"] = CommandStatus
		statusData["command_id"] = commandID
		statusData["status"] = StatusSuccess

		wsm.sendResponse(c, MessageTypeCommandResponse, statusData)
	default:
		log.Printf("Unknown command: %s", command)
		wsm.sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
			"command":    command,
			"command_id": commandID,
			"status":     StatusError,
			"message":    "Unknown command",
		})
	}
}

func (wsm *WebSocketManager) handleDeactivated(_ *websocket.Conn, message map[string]interface{}) {
	deactivatedMessage := "Device deactivated by server"
	if msg, ok := message["message"].(string); ok {
		deactivatedMessage = msg
	}

	log.Printf("DEACTIVATED: %s", deactivatedMessage)
	log.Println("Device has been deactivated by the server. Resetting pairing state...")

	// Close the WebSocket connection immediately
	if wsm.IsConnected() {
		if err := wsm.DisconnectWebSocket(nil, false); err != nil {
			log.Printf("Failed to disconnect WebSocket: %v", err)
		} else {
			log.Println("WebSocket disconnected successfully")
		}
	}

	// Remove the state file to reset pairing
	if state.HasState() {
		if err := state.DeleteState(); err != nil {
			log.Printf("Failed to delete state file: %v", err)
		} else {
			log.Println("State file deleted successfully - pairing reset")
		}
	}
}

func (wsm *WebSocketManager) handleError(_ *websocket.Conn, message map[string]interface{}) {
	errorMessage := "Unknown error from server"
	if msg, ok := message["message"].(string); ok {
		errorMessage = msg
	}

	timestamp := "unknown"
	if ts, ok := message["timestamp"]; ok {
		if tsInt, ok := ts.(float64); ok {
			timestamp = time.Unix(int64(tsInt), 0).Format(time.RFC3339)
		} else if tsStr, ok := ts.(string); ok {
			timestamp = tsStr
		}
	}

	log.Printf("ERROR from server: %s (timestamp: %s)", errorMessage, timestamp)
}

func (wsm *WebSocketManager) sendResponse(c *websocket.Conn, messageType MessageType, data map[string]interface{}) error {
	response := map[string]interface{}{
		"type": string(messageType),
	}

	// Merge data into response
	for key, value := range data {
		response[key] = value
	}

	// Add timestamp if not already present
	if _, hasTimestamp := response["timestamp"]; !hasTimestamp {
		response["timestamp"] = time.Now().Unix()
	}

	// Check if we have a session key for encryption
	sessionKey := state.GetSessionKey()

	if sessionKey == "" {
		return fmt.Errorf("no session key available, cannot send %s message", messageType)
	}

	// Encrypt the message
	encryptedResponse, err := utils.EncryptWebSocketMessage(response, sessionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt %s message: %w", messageType, err)
	}

	log.Printf("Sending encrypted %s message", messageType)
	err = c.WriteJSON(encryptedResponse)
	if err != nil {
		return fmt.Errorf("failed to send %s message: %w", messageType, err)
	}
	return nil
}

func (wsm *WebSocketManager) DisconnectWebSocket(c *websocket.Conn, sendMessage bool) error {
	// If no connection provided, use global connection
	if c == nil {
		c = wsm.GetConnection()
	}

	if c == nil {
		return nil // No connection to close
	}

	if !wsm.IsConnected() {
		log.Println("WebSocket connection already closed or not connected")
		wsm.clearConnection()
		return nil
	}

	if sendMessage {
		// Send disconnect message to server before closing
		// Try to send encrypted disconnect message using sendResponse
		if err := wsm.sendResponse(c, MessageTypeDisconnect, map[string]interface{}{
			"message": "client_disconnecting",
		}); err != nil {
			log.Printf("Failed to send encrypted disconnect message: %v", err)
		} else {
			log.Println("Sent encrypted disconnect message to server")
		}
	}

	// Send close message
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Client disconnecting"))
	if err != nil {
		log.Printf("Failed to send close message: %v", err)
	}

	// Wait for the close acknowledgment
	time.Sleep(5 * time.Second)

	if !wsm.IsConnected() {
		log.Println("WebSocket connection already closed or not connected")
		wsm.clearConnection()
		return nil
	}

	err = c.Close()
	if err != nil {
		log.Printf("Failed to close WebSocket connection: %v", err)
	}

	// Clear global connection variables
	wsm.clearConnection()

	log.Println("WebSocket connection closed successfully")
	return err
}

// ShutdownWebSocket gracefully disconnects and prevents reconnection
func (wsm *WebSocketManager) ShutdownWebSocket(sendMessage bool) error {
	log.Println("Initiating WebSocket shutdown...")

	// Set shutdown flag to prevent reconnection
	wsm.SetShutdown()

	// Disconnect the current connection
	return wsm.DisconnectWebSocket(nil, sendMessage)
}

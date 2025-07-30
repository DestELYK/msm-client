package ws

import (
	"encoding/json"
	"log"
	"net/http"
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
var (
	Connection *websocket.Conn
	Headers    http.Header
	mu         sync.RWMutex
	connected  bool
	shutdown   bool // Flag to prevent reconnection during shutdown
	// TestMode prevents actual command execution during testing
	TestMode bool
	// Current client configuration
	clientConfig config.ClientConfig
)

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

// isTestEnvironment checks if we're running in a test environment
func isTestEnvironment() bool {
	return TestMode || os.Getenv("GO_TEST_MODE") == "1"
}

// SetTestMode enables or disables test mode
func SetTestMode(enabled bool) {
	mu.Lock()
	defer mu.Unlock()
	TestMode = enabled
}

// GetConnection returns the current WebSocket connection (thread-safe)
func GetConnection() *websocket.Conn {
	mu.RLock()
	defer mu.RUnlock()
	return Connection
}

// IsConnected returns whether the WebSocket is currently connected (thread-safe)
func IsConnected() bool {
	mu.RLock()
	defer mu.RUnlock()
	return connected && Connection != nil
}

// SetShutdown sets the shutdown flag to prevent reconnection (thread-safe)
func SetShutdown() {
	mu.Lock()
	defer mu.Unlock()
	shutdown = true
}

// IsShutdown returns whether shutdown has been initiated (thread-safe)
func IsShutdown() bool {
	mu.RLock()
	defer mu.RUnlock()
	return shutdown
}

// ResetShutdown clears the shutdown flag to allow reconnection (thread-safe)
func ResetShutdown() {
	mu.Lock()
	defer mu.Unlock()
	shutdown = false
}

// setConnection sets the global connection and headers (thread-safe)
func setConnection(conn *websocket.Conn, headers http.Header) {
	mu.Lock()
	defer mu.Unlock()
	Connection = conn
	Headers = headers
	connected = true
}

// clearConnection clears the global connection and headers (thread-safe)
func clearConnection() {
	mu.Lock()
	defer mu.Unlock()
	if Connection != nil {
		Connection.Close()
	}
	Connection = nil
	Headers = nil
	connected = false
}

// SendMessage sends a message using the global connection (thread-safe)
func SendMessage(messageType MessageType, data map[string]interface{}) error {
	conn := GetConnection()
	if conn == nil {
		return websocket.ErrCloseSent // Connection not available
	}

	return sendResponse(conn, messageType, data)
}

func ConnectWebSocket(cfg config.ClientConfig, serverWs string, token string) {
	// Store config globally for use in command handling
	mu.Lock()
	clientConfig = cfg
	mu.Unlock()

	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)

	backoff := time.Second
	for {
		// Check if shutdown has been initiated
		if IsShutdown() {
			log.Println("Shutdown initiated, stopping WebSocket connection attempts")
			return
		}

		// Check if state file still exists before attempting connection
		if !state.HasState() {
			log.Println("State file no longer exists, stopping WebSocket connection")
			return
		}

		c, _, err := websocket.DefaultDialer.Dial(serverWs, headers)
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
		setConnection(c, headers)

		// Channel to signal when connection should close
		done := make(chan struct{})
		stateDeleted := make(chan struct{})
		deactivated := make(chan struct{})
		var closeOnce sync.Once

		// Goroutine to listen for incoming messages
		go func() {
			defer closeOnce.Do(func() { close(done) })
			for {
				var message map[string]interface{}
				err := c.ReadJSON(&message)
				if err != nil {
					log.Printf("Read failed: %v", err)
					return
				}

				// Check if this is a deactivated message
				if msgType, ok := message["type"].(string); ok && MessageType(msgType) == MessageTypeDeactivated {
					handleDeactivated(c, message)
					close(deactivated)
					return
				}

				// Handle other incoming messages
				handleMessage(c, message)
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
					log.Println("Sending status update to server")
					statusData := map[string]any{
						"type":       MessageTypeStatus,
						"clientId":   cfg.ClientID,
						"uptime":     utils.GetUptime(),
						"interfaces": utils.GetNetworkInterfaces(),
						"timestamp":  time.Now().Format(time.RFC3339),
					}

					err := c.WriteJSON(statusData)
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
					if !state.HasState() {
						log.Println("State file no longer exists, closing WebSocket connection to restart pairing")
						close(stateDeleted)
						closeOnce.Do(func() { close(done) })
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
			clearConnection()
			// Check if shutdown has been initiated before attempting reconnect
			if IsShutdown() {
				log.Println("WebSocket connection closed during shutdown, not reconnecting")
				return
			}
			log.Println("WebSocket connection closed, attempting to reconnect...")
		case <-stateDeleted:
			clearConnection()
			log.Println("State file deleted, closing WebSocket to restart pairing server")
			return // Exit function to allow pairing server restart
		case <-deactivated:
			clearConnection()
			log.Println("Device deactivated by server, exiting WebSocket connection")
			return // Exit function to stop WebSocket and allow pairing restart
		}
	}
}

func handleMessage(c *websocket.Conn, message map[string]interface{}) {
	msgType, ok := message["type"].(string)
	if !ok {
		log.Printf("Received message without type: %v", message)
		return
	}

	switch MessageType(msgType) {
	case MessageTypePing:
		log.Println("Received ping from server")
		// Respond to ping with pong
		sendResponse(c, MessageTypePong, map[string]interface{}{
			"timestamp": time.Now().Unix(),
		})
	case MessageTypeCommand:
		handleCommand(c, message)
	case MessageTypeDeactivated:
		handleDeactivated(c, message)
	default:
		log.Printf("Received unknown message type '%s': %v", msgType, message)
	}
}

func handleCommand(c *websocket.Conn, message map[string]interface{}) {
	command, ok := message["command"].(string)
	if !ok {
		log.Printf("Command message missing 'command' field: %v", message)
		sendResponse(c, MessageTypeError, map[string]interface{}{
			"message": "Command field missing",
		})
		return
	}

	log.Printf("Received command: %s", command)

	// Check if commands are disabled
	mu.RLock()
	commandsDisabled := clientConfig.DisableCommands
	mu.RUnlock()

	if commandsDisabled {
		log.Printf("Command execution disabled, rejecting command: %s", command)
		sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
			"command": command,
			"status":  StatusError,
			"message": "Command execution is disabled on this client",
		})
		return
	}

	switch CommandType(command) {
	case CommandReboot:
		log.Println("Reboot command received - would reboot system")
		sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
			"command": CommandReboot,
			"status":  StatusAcknowledged,
			"message": "Reboot command received, system would reboot",
		})

		// Only execute actual reboot command if not in test environment
		if !isTestEnvironment() {
			cmd := exec.Command("reboot")
			err := cmd.Run()
			if err != nil {
				log.Printf("Failed to execute reboot command: %v", err)
				sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
					"command": CommandReboot,
					"status":  StatusError,
					"message": "Failed to execute reboot command",
				})
				return
			}
		} else {
			log.Println("Test mode: Reboot command acknowledged but not executed")
		}
	case CommandStatus:
		log.Println("Status request received")
		sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
			"command":   CommandStatus,
			"status":    StatusSuccess,
			"uptime":    time.Now().Unix(),
			"timestamp": time.Now().Format(time.RFC3339),
		})
	default:
		log.Printf("Unknown command: %s", command)
		sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
			"command": command,
			"status":  StatusError,
			"message": "Unknown command",
		})
	}
}

func handleDeactivated(_ *websocket.Conn, message map[string]interface{}) {
	deactivatedMessage := "Device deactivated by server"
	if msg, ok := message["message"].(string); ok {
		deactivatedMessage = msg
	}

	log.Printf("DEACTIVATED: %s", deactivatedMessage)
	log.Println("Device has been deactivated by the server. Resetting pairing state...")

	// Close the WebSocket connection immediately
	clearConnection()

	// Remove the state file to reset pairing
	if state.HasState() {
		if err := state.DeleteState(); err != nil {
			log.Printf("Failed to delete state file: %v", err)
		} else {
			log.Println("State file deleted successfully - pairing reset")
		}
	}
}

func sendResponse(c *websocket.Conn, messageType MessageType, data map[string]interface{}) error {
	response := map[string]interface{}{
		"type": string(messageType),
	}

	// Merge data into response
	for key, value := range data {
		response[key] = value
	}

	err := c.WriteJSON(response)
	if err != nil {
		log.Printf("Failed to send response: %v", err)
	}
	return err
}

func DisconnectWebSocket(c *websocket.Conn) error {
	// If no connection provided, use global connection
	if c == nil {
		c = GetConnection()
	}

	if c == nil {
		return nil // No connection to close
	}

	// Send disconnect message to server before closing
	disconnectMsg := map[string]interface{}{
		"type":      "disconnect",
		"message":   "client_disconnecting",
		"timestamp": time.Now().Unix(),
	}

	// Try to send disconnect message (but don't fail if it doesn't work)
	if msgBytes, err := json.Marshal(disconnectMsg); err == nil {
		if err := c.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			log.Printf("Failed to send disconnect message: %v", err)
		} else {
			log.Println("Sent disconnect message to server")
		}
	}

	// Send close message
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Client disconnecting"))
	if err != nil {
		log.Printf("Failed to send close message: %v", err)
	}

	// Wait for the close acknowledgment
	time.Sleep(5 * time.Second)

	err = c.Close()
	if err != nil {
		log.Printf("Failed to close WebSocket connection: %v", err)
	}

	// Clear global connection variables
	clearConnection()

	log.Println("WebSocket connection closed successfully")
	return err
}

// ShutdownWebSocket gracefully disconnects and prevents reconnection
func ShutdownWebSocket() error {
	log.Println("Initiating WebSocket shutdown...")

	// Set shutdown flag to prevent reconnection
	SetShutdown()

	// Disconnect the current connection
	return DisconnectWebSocket(nil)
}

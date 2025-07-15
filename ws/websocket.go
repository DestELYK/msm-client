package ws

import (
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/gorilla/websocket"

	"msm-client/config"
)

// MessageType represents the type of WebSocket message
type MessageType string

const (
	// Incoming message types
	MessageTypePing    MessageType = "ping"
	MessageTypeCommand MessageType = "command"

	// Outgoing message types
	MessageTypePong            MessageType = "pong"
	MessageTypeStatus          MessageType = "status"
	MessageTypeCommandResponse MessageType = "command_response"
	MessageTypeError           MessageType = "error"
)

// CommandType represents the type of command
type CommandType string

const (
	CommandReboot  CommandType = "reboot"
	CommandRestart CommandType = "restart"
	CommandStatus  CommandType = "status"
)

// ResponseStatus represents the status of a command response
type ResponseStatus string

const (
	StatusAcknowledged ResponseStatus = "acknowledged"
	StatusSuccess      ResponseStatus = "success"
	StatusError        ResponseStatus = "error"
)

func ConnectWebSocket(cfg config.ClientConfig, serverWs string, token string) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)

	backoff := time.Second
	for {
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

		// Channel to signal when connection should close
		done := make(chan struct{})

		// Goroutine to listen for incoming messages
		go func() {
			defer close(done)
			for {
				var message map[string]interface{}
				err := c.ReadJSON(&message)
				if err != nil {
					log.Printf("Read failed: %v", err)
					return
				}

				// Handle incoming messages
				handleMessage(c, message)
			}
		}()

		// Goroutine to send periodic status updates
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					err := c.WriteJSON(map[string]any{
						"type":     MessageTypeStatus,
						"clientId": cfg.ClientID,
						"uptime":   time.Now().Unix(),
					})
					if err != nil {
						log.Printf("Write failed: %v", err)
						return
					}
				case <-done:
					return
				}
			}
		}()

		// Wait for either goroutine to finish (indicating connection loss)
		<-done
		c.Close()
		log.Println("WebSocket connection closed, attempting to reconnect...")
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

	switch CommandType(command) {
	case CommandReboot:
		log.Println("Reboot command received - would reboot system")
		sendResponse(c, MessageTypeCommandResponse, map[string]interface{}{
			"command": CommandReboot,
			"status":  StatusAcknowledged,
			"message": "Reboot command received, system would reboot",
		})
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

func sendResponse(c *websocket.Conn, messageType MessageType, data map[string]interface{}) {
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
}

package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MessageCrypto handles encryption/decryption of WebSocket messages
type MessageCrypto struct{}

// EncryptedEnvelope represents an encrypted message envelope
type EncryptedEnvelope struct {
	Type      string      `json:"type"`
	Encrypted bool        `json:"encrypted"`
	Payload   string      `json:"payload"`
	Timestamp interface{} `json:"timestamp,omitempty"`
}

// NewMessageCrypto creates a new MessageCrypto instance
func NewMessageCrypto() *MessageCrypto {
	return &MessageCrypto{}
}

// EncryptMessage encrypts a WebSocket message using the session key
func (mc *MessageCrypto) EncryptMessage(message map[string]interface{}, sessionKeyB64 string) (string, error) {
	// Convert message to JSON
	messageJSON, err := json.Marshal(message)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message: %w", err)
	}

	// Decode session key
	sessionKey, err := base64.StdEncoding.DecodeString(sessionKeyB64)
	if err != nil {
		return "", fmt.Errorf("failed to decode session key: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Apply PKCS7 padding
	paddedMessage := mc.applyPKCS7Padding(messageJSON, aes.BlockSize)

	// Generate random IV
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("failed to generate IV: %w", err)
	}

	// Encrypt using CBC mode
	mode := cipher.NewCBCEncrypter(block, iv)
	encryptedData := make([]byte, len(paddedMessage))
	mode.CryptBlocks(encryptedData, paddedMessage)

	// Combine IV and encrypted data
	encryptedMessage := append(iv, encryptedData...)

	// Return base64-encoded result
	return base64.StdEncoding.EncodeToString(encryptedMessage), nil
}

// DecryptMessage decrypts a WebSocket message using the session key
func (mc *MessageCrypto) DecryptMessage(encryptedMessageB64, sessionKeyB64 string) (map[string]interface{}, error) {
	// Decode encrypted message
	encryptedMessage, err := base64.StdEncoding.DecodeString(encryptedMessageB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted message: %w", err)
	}

	// Decode session key
	sessionKey, err := base64.StdEncoding.DecodeString(sessionKeyB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode session key: %w", err)
	}

	if len(encryptedMessage) < aes.BlockSize {
		return nil, errors.New("encrypted message too short")
	}

	// Extract IV and encrypted data
	iv := encryptedMessage[:aes.BlockSize]
	encryptedData := encryptedMessage[aes.BlockSize:]

	// Create AES cipher
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Decrypt using CBC mode
	mode := cipher.NewCBCDecrypter(block, iv)
	paddedMessage := make([]byte, len(encryptedData))
	mode.CryptBlocks(paddedMessage, encryptedData)

	// Remove PKCS7 padding
	messageJSON, err := mc.removePKCS7Padding(paddedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to remove padding: %w", err)
	}

	// Parse JSON
	var message map[string]interface{}
	if err := json.Unmarshal(messageJSON, &message); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return message, nil
}

// CreateEncryptedEnvelope creates an encrypted envelope for a message
func (mc *MessageCrypto) CreateEncryptedEnvelope(message map[string]interface{}, sessionKeyB64 string) (map[string]interface{}, error) {
	encryptedPayload, err := mc.EncryptMessage(message, sessionKeyB64)
	if err != nil {
		return nil, err
	}

	envelope := map[string]interface{}{
		"type":      "encrypted",
		"encrypted": true,
		"payload":   encryptedPayload,
	}

	// Keep timestamp unencrypted for validation
	if timestamp, exists := message["timestamp"]; exists {
		envelope["timestamp"] = timestamp
	}

	return envelope, nil
}

// ExtractFromEncryptedEnvelope extracts and decrypts a message from an envelope
func (mc *MessageCrypto) ExtractFromEncryptedEnvelope(envelope map[string]interface{}, sessionKeyB64 string) (map[string]interface{}, error) {
	encrypted, ok := envelope["encrypted"].(bool)
	if !ok || !encrypted {
		return nil, errors.New("message is not encrypted")
	}

	payload, ok := envelope["payload"].(string)
	if !ok {
		return nil, errors.New("no encrypted payload found")
	}

	return mc.DecryptMessage(payload, sessionKeyB64)
}

// IsEncryptedMessage checks if a message is encrypted
func (mc *MessageCrypto) IsEncryptedMessage(message map[string]interface{}) bool {
	msgType, typeOk := message["type"].(string)
	encrypted, encOk := message["encrypted"].(bool)
	return typeOk && encOk && msgType == "encrypted" && encrypted
}

// applyPKCS7Padding applies PKCS7 padding to data
func (mc *MessageCrypto) applyPKCS7Padding(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// removePKCS7Padding removes PKCS7 padding from data
func (mc *MessageCrypto) removePKCS7Padding(data []byte) ([]byte, error) {
	length := len(data)
	if length == 0 {
		return nil, errors.New("data is empty")
	}

	padding := int(data[length-1])
	if padding > length || padding == 0 {
		return nil, errors.New("invalid padding")
	}

	// Verify padding
	for i := length - padding; i < length; i++ {
		if data[i] != byte(padding) {
			return nil, errors.New("invalid padding")
		}
	}

	return data[:length-padding], nil
}

// Global instance
var messageCrypto = NewMessageCrypto()

// EncryptWebSocketMessage convenience function to encrypt a WebSocket message
func EncryptWebSocketMessage(message map[string]interface{}, sessionKeyB64 string) (map[string]interface{}, error) {
	return messageCrypto.CreateEncryptedEnvelope(message, sessionKeyB64)
}

// DecryptWebSocketMessage convenience function to decrypt a WebSocket message
func DecryptWebSocketMessage(envelope map[string]interface{}, sessionKeyB64 string) (map[string]interface{}, error) {
	return messageCrypto.ExtractFromEncryptedEnvelope(envelope, sessionKeyB64)
}

// IsEncryptedWebSocketMessage convenience function to check if a message is encrypted
func IsEncryptedWebSocketMessage(message map[string]interface{}) bool {
	return messageCrypto.IsEncryptedMessage(message)
}

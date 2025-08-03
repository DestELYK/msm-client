package utils

import (
	"crypto/aes"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestNewMessageCrypto(t *testing.T) {
	t.Run("Create new instance", func(t *testing.T) {
		mc := NewMessageCrypto()
		if mc == nil {
			t.Error("NewMessageCrypto should not return nil")
		}
	})

	t.Run("Multiple instances are independent", func(t *testing.T) {
		mc1 := NewMessageCrypto()
		mc2 := NewMessageCrypto()
		if mc1 == mc2 {
			t.Error("Multiple instances should be independent")
		}
	})
}

func TestApplyPKCS7Padding(t *testing.T) {
	mc := NewMessageCrypto()

	tests := []struct {
		name      string
		data      []byte
		blockSize int
		expected  int // expected length after padding
	}{
		{
			name:      "Empty data",
			data:      []byte{},
			blockSize: 16,
			expected:  16,
		},
		{
			name:      "Data shorter than block size",
			data:      []byte("hello"),
			blockSize: 16,
			expected:  16,
		},
		{
			name:      "Data equal to block size",
			data:      make([]byte, 16),
			blockSize: 16,
			expected:  32, // Should add full block of padding
		},
		{
			name:      "Data longer than block size",
			data:      make([]byte, 20),
			blockSize: 16,
			expected:  32,
		},
		{
			name:      "Different block size",
			data:      []byte("test"),
			blockSize: 8,
			expected:  8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mc.applyPKCS7Padding(tt.data, tt.blockSize)
			if len(result) != tt.expected {
				t.Errorf("Expected length %d, got %d", tt.expected, len(result))
			}

			// Verify padding is correct
			paddingLen := int(result[len(result)-1])
			if paddingLen <= 0 || paddingLen > tt.blockSize {
				t.Errorf("Invalid padding length: %d", paddingLen)
			}

			// Verify all padding bytes are the same
			for i := len(result) - paddingLen; i < len(result); i++ {
				if result[i] != byte(paddingLen) {
					t.Errorf("Invalid padding byte at position %d: expected %d, got %d", i, paddingLen, result[i])
				}
			}

			// Verify original data is preserved
			if len(tt.data) > 0 && !reflect.DeepEqual(result[:len(tt.data)], tt.data) {
				t.Error("Original data not preserved in padded result")
			}
		})
	}
}

func TestRemovePKCS7Padding(t *testing.T) {
	mc := NewMessageCrypto()

	t.Run("Valid padding", func(t *testing.T) {
		originalData := []byte("hello world")
		paddedData := mc.applyPKCS7Padding(originalData, 16)

		result, err := mc.removePKCS7Padding(paddedData)
		if err != nil {
			t.Errorf("removePKCS7Padding failed: %v", err)
		}

		if !reflect.DeepEqual(result, originalData) {
			t.Errorf("Expected %v, got %v", originalData, result)
		}
	})

	t.Run("Empty data", func(t *testing.T) {
		_, err := mc.removePKCS7Padding([]byte{})
		if err == nil {
			t.Error("Expected error for empty data")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("Expected 'empty' error, got: %v", err)
		}
	})

	t.Run("Invalid padding length", func(t *testing.T) {
		// Create data with invalid padding (too long)
		invalidData := make([]byte, 16)
		invalidData[15] = 17 // Padding length > block size

		_, err := mc.removePKCS7Padding(invalidData)
		if err == nil {
			t.Error("Expected error for invalid padding length")
		}
	})

	t.Run("Zero padding", func(t *testing.T) {
		invalidData := make([]byte, 16)
		invalidData[15] = 0 // Zero padding

		_, err := mc.removePKCS7Padding(invalidData)
		if err == nil {
			t.Error("Expected error for zero padding")
		}
	})

	t.Run("Inconsistent padding", func(t *testing.T) {
		invalidData := make([]byte, 16)
		for i := 12; i < 16; i++ {
			invalidData[i] = 4
		}
		invalidData[14] = 3 // Different padding byte

		_, err := mc.removePKCS7Padding(invalidData)
		if err == nil {
			t.Error("Expected error for inconsistent padding")
		}
	})
}

func TestEncryptMessage(t *testing.T) {
	mc := NewMessageCrypto()

	// Create a valid 32-byte session key
	sessionKey := make([]byte, 32)
	for i := range sessionKey {
		sessionKey[i] = byte(i)
	}
	sessionKeyB64 := base64.StdEncoding.EncodeToString(sessionKey)

	t.Run("Valid message encryption", func(t *testing.T) {
		message := map[string]interface{}{
			"type":    "test",
			"content": "hello world",
			"number":  42,
		}

		encrypted, err := mc.EncryptMessage(message, sessionKeyB64)
		if err != nil {
			t.Errorf("EncryptMessage failed: %v", err)
		}

		if encrypted == "" {
			t.Error("Encrypted message should not be empty")
		}

		// Verify it's valid base64
		_, err = base64.StdEncoding.DecodeString(encrypted)
		if err != nil {
			t.Errorf("Encrypted message is not valid base64: %v", err)
		}
	})

	t.Run("Invalid session key", func(t *testing.T) {
		message := map[string]interface{}{"test": "data"}

		_, err := mc.EncryptMessage(message, "invalid-base64!")
		if err == nil {
			t.Error("Expected error for invalid session key")
		}
		if !strings.Contains(err.Error(), "decode") {
			t.Errorf("Expected decode error, got: %v", err)
		}
	})

	t.Run("Wrong session key length", func(t *testing.T) {
		// Create invalid key length
		shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
		message := map[string]interface{}{"test": "data"}

		_, err := mc.EncryptMessage(message, shortKey)
		if err == nil {
			t.Error("Expected error for wrong key length")
		}
	})

	t.Run("Unmarshalable message", func(t *testing.T) {
		// Create message with unmarshalable content
		message := map[string]interface{}{
			"invalid": func() {}, // Functions can't be marshaled to JSON
		}

		_, err := mc.EncryptMessage(message, sessionKeyB64)
		if err == nil {
			t.Error("Expected error for unmarshalable message")
		}
		if !strings.Contains(err.Error(), "marshal") {
			t.Errorf("Expected marshal error, got: %v", err)
		}
	})
}

func TestDecryptMessage(t *testing.T) {
	mc := NewMessageCrypto()

	// Create a valid 32-byte session key
	sessionKey := make([]byte, 32)
	for i := range sessionKey {
		sessionKey[i] = byte(i)
	}
	sessionKeyB64 := base64.StdEncoding.EncodeToString(sessionKey)

	t.Run("Valid message decryption", func(t *testing.T) {
		originalMessage := map[string]interface{}{
			"type":    "test",
			"content": "hello world",
			"number":  float64(42), // JSON numbers become float64
		}

		// Encrypt first
		encrypted, err := mc.EncryptMessage(originalMessage, sessionKeyB64)
		if err != nil {
			t.Fatalf("Encryption failed: %v", err)
		}

		// Then decrypt
		decrypted, err := mc.DecryptMessage(encrypted, sessionKeyB64)
		if err != nil {
			t.Errorf("DecryptMessage failed: %v", err)
		}

		// Verify decrypted matches original
		if !reflect.DeepEqual(decrypted, originalMessage) {
			t.Errorf("Decrypted message doesn't match original.\nOriginal: %v\nDecrypted: %v", originalMessage, decrypted)
		}
	})

	t.Run("Invalid encrypted message base64", func(t *testing.T) {
		_, err := mc.DecryptMessage("invalid-base64!", sessionKeyB64)
		if err == nil {
			t.Error("Expected error for invalid base64")
		}
		if !strings.Contains(err.Error(), "decode") {
			t.Errorf("Expected decode error, got: %v", err)
		}
	})

	t.Run("Invalid session key", func(t *testing.T) {
		validEncrypted := base64.StdEncoding.EncodeToString(make([]byte, 32))
		_, err := mc.DecryptMessage(validEncrypted, "invalid-base64!")
		if err == nil {
			t.Error("Expected error for invalid session key")
		}
	})

	t.Run("Message too short", func(t *testing.T) {
		shortMessage := base64.StdEncoding.EncodeToString([]byte("short"))
		_, err := mc.DecryptMessage(shortMessage, sessionKeyB64)
		if err == nil {
			t.Error("Expected error for message too short")
		}
		if !strings.Contains(err.Error(), "too short") {
			t.Errorf("Expected 'too short' error, got: %v", err)
		}
	})

	t.Run("Wrong session key length", func(t *testing.T) {
		shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
		validEncrypted := base64.StdEncoding.EncodeToString(make([]byte, 32))

		_, err := mc.DecryptMessage(validEncrypted, shortKey)
		if err == nil {
			t.Error("Expected error for wrong key length")
		}
	})
}

func TestCreateEncryptedEnvelope(t *testing.T) {
	mc := NewMessageCrypto()

	// Create a valid session key
	sessionKey := make([]byte, 32)
	sessionKeyB64 := base64.StdEncoding.EncodeToString(sessionKey)

	t.Run("Valid envelope creation", func(t *testing.T) {
		message := map[string]interface{}{
			"type":      "test",
			"content":   "hello",
			"timestamp": 1234567890,
		}

		envelope, err := mc.CreateEncryptedEnvelope(message, sessionKeyB64)
		if err != nil {
			t.Errorf("CreateEncryptedEnvelope failed: %v", err)
		}

		// Verify envelope structure
		if envelope["type"] != "encrypted" {
			t.Error("Envelope type should be 'encrypted'")
		}
		if envelope["encrypted"] != true {
			t.Error("Envelope encrypted flag should be true")
		}
		if envelope["payload"] == "" {
			t.Error("Envelope payload should not be empty")
		}
		if envelope["timestamp"] != 1234567890 {
			t.Error("Envelope should preserve timestamp")
		}
	})

	t.Run("Message without timestamp", func(t *testing.T) {
		message := map[string]interface{}{
			"type":    "test",
			"content": "hello",
		}

		envelope, err := mc.CreateEncryptedEnvelope(message, sessionKeyB64)
		if err != nil {
			t.Errorf("CreateEncryptedEnvelope failed: %v", err)
		}

		// Should not have timestamp
		if _, exists := envelope["timestamp"]; exists {
			t.Error("Envelope should not have timestamp when original doesn't")
		}
	})

	t.Run("Encryption error propagation", func(t *testing.T) {
		message := map[string]interface{}{
			"invalid": func() {}, // Unmarshalable
		}

		_, err := mc.CreateEncryptedEnvelope(message, sessionKeyB64)
		if err == nil {
			t.Error("Expected error for unmarshalable message")
		}
	})
}

func TestExtractFromEncryptedEnvelope(t *testing.T) {
	mc := NewMessageCrypto()

	// Create a valid session key
	sessionKey := make([]byte, 32)
	sessionKeyB64 := base64.StdEncoding.EncodeToString(sessionKey)

	t.Run("Valid envelope extraction", func(t *testing.T) {
		originalMessage := map[string]interface{}{
			"type":    "test",
			"content": "hello world",
			"number":  float64(42),
		}

		// Create envelope
		envelope, err := mc.CreateEncryptedEnvelope(originalMessage, sessionKeyB64)
		if err != nil {
			t.Fatalf("Failed to create envelope: %v", err)
		}

		// Extract message
		extracted, err := mc.ExtractFromEncryptedEnvelope(envelope, sessionKeyB64)
		if err != nil {
			t.Errorf("ExtractFromEncryptedEnvelope failed: %v", err)
		}

		if !reflect.DeepEqual(extracted, originalMessage) {
			t.Errorf("Extracted message doesn't match original.\nOriginal: %v\nExtracted: %v", originalMessage, extracted)
		}
	})

	t.Run("Non-encrypted envelope", func(t *testing.T) {
		envelope := map[string]interface{}{
			"type":      "plain",
			"encrypted": false,
			"payload":   "test",
		}

		_, err := mc.ExtractFromEncryptedEnvelope(envelope, sessionKeyB64)
		if err == nil {
			t.Error("Expected error for non-encrypted envelope")
		}
		if !strings.Contains(err.Error(), "not encrypted") {
			t.Errorf("Expected 'not encrypted' error, got: %v", err)
		}
	})

	t.Run("Missing encrypted flag", func(t *testing.T) {
		envelope := map[string]interface{}{
			"type":    "encrypted",
			"payload": "test",
		}

		_, err := mc.ExtractFromEncryptedEnvelope(envelope, sessionKeyB64)
		if err == nil {
			t.Error("Expected error for missing encrypted flag")
		}
	})

	t.Run("Missing payload", func(t *testing.T) {
		envelope := map[string]interface{}{
			"type":      "encrypted",
			"encrypted": true,
		}

		_, err := mc.ExtractFromEncryptedEnvelope(envelope, sessionKeyB64)
		if err == nil {
			t.Error("Expected error for missing payload")
		}
		if !strings.Contains(err.Error(), "no encrypted payload") {
			t.Errorf("Expected 'no encrypted payload' error, got: %v", err)
		}
	})

	t.Run("Invalid payload type", func(t *testing.T) {
		envelope := map[string]interface{}{
			"type":      "encrypted",
			"encrypted": true,
			"payload":   123, // Should be string
		}

		_, err := mc.ExtractFromEncryptedEnvelope(envelope, sessionKeyB64)
		if err == nil {
			t.Error("Expected error for invalid payload type")
		}
	})
}

func TestIsEncryptedMessage(t *testing.T) {
	mc := NewMessageCrypto()

	tests := []struct {
		name     string
		message  map[string]interface{}
		expected bool
	}{
		{
			name: "Valid encrypted message",
			message: map[string]interface{}{
				"type":      "encrypted",
				"encrypted": true,
				"payload":   "test",
			},
			expected: true,
		},
		{
			name: "Non-encrypted message",
			message: map[string]interface{}{
				"type":      "encrypted",
				"encrypted": false,
				"payload":   "test",
			},
			expected: false,
		},
		{
			name: "Wrong type",
			message: map[string]interface{}{
				"type":      "plain",
				"encrypted": true,
				"payload":   "test",
			},
			expected: false,
		},
		{
			name: "Missing type",
			message: map[string]interface{}{
				"encrypted": true,
				"payload":   "test",
			},
			expected: false,
		},
		{
			name: "Missing encrypted flag",
			message: map[string]interface{}{
				"type":    "encrypted",
				"payload": "test",
			},
			expected: false,
		},
		{
			name: "Invalid type field",
			message: map[string]interface{}{
				"type":      123,
				"encrypted": true,
				"payload":   "test",
			},
			expected: false,
		},
		{
			name: "Invalid encrypted field",
			message: map[string]interface{}{
				"type":      "encrypted",
				"encrypted": "true",
				"payload":   "test",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mc.IsEncryptedMessage(tt.message)
			if result != tt.expected {
				t.Errorf("IsEncryptedMessage() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestConvenienceFunctions(t *testing.T) {
	// Create a valid session key
	sessionKey := make([]byte, 32)
	sessionKeyB64 := base64.StdEncoding.EncodeToString(sessionKey)

	t.Run("EncryptWebSocketMessage", func(t *testing.T) {
		message := map[string]interface{}{
			"type":    "test",
			"content": "hello",
		}

		envelope, err := EncryptWebSocketMessage(message, sessionKeyB64)
		if err != nil {
			t.Errorf("EncryptWebSocketMessage failed: %v", err)
		}

		if envelope["type"] != "encrypted" {
			t.Error("Envelope should have type 'encrypted'")
		}
	})

	t.Run("DecryptWebSocketMessage", func(t *testing.T) {
		originalMessage := map[string]interface{}{
			"type":    "test",
			"content": "hello",
			"number":  float64(42),
		}

		// Encrypt first
		envelope, err := EncryptWebSocketMessage(originalMessage, sessionKeyB64)
		if err != nil {
			t.Fatalf("Encryption failed: %v", err)
		}

		// Decrypt
		decrypted, err := DecryptWebSocketMessage(envelope, sessionKeyB64)
		if err != nil {
			t.Errorf("DecryptWebSocketMessage failed: %v", err)
		}

		if !reflect.DeepEqual(decrypted, originalMessage) {
			t.Error("Decrypted message doesn't match original")
		}
	})

	t.Run("IsEncryptedWebSocketMessage", func(t *testing.T) {
		encryptedMsg := map[string]interface{}{
			"type":      "encrypted",
			"encrypted": true,
			"payload":   "test",
		}

		plainMsg := map[string]interface{}{
			"type":    "plain",
			"content": "test",
		}

		if !IsEncryptedWebSocketMessage(encryptedMsg) {
			t.Error("Should identify encrypted message")
		}

		if IsEncryptedWebSocketMessage(plainMsg) {
			t.Error("Should not identify plain message as encrypted")
		}
	})
}

func TestEncryptionDecryptionRoundTrip(t *testing.T) {
	mc := NewMessageCrypto()

	// Create multiple session keys to test with different keys
	sessionKeys := make([]string, 3)
	for i := range sessionKeys {
		key := make([]byte, 32)
		for j := range key {
			key[j] = byte(i*10 + j)
		}
		sessionKeys[i] = base64.StdEncoding.EncodeToString(key)
	}

	testMessages := []map[string]interface{}{
		{
			"type": "simple",
			"text": "hello world",
		},
		{
			"type":   "complex",
			"text":   "hello world",
			"number": 42,
			"bool":   true,
			"array":  []interface{}{1, 2, 3},
			"object": map[string]interface{}{"nested": "value"},
		},
		{
			"type": "unicode",
			"text": "Hello 世界 🌍",
		},
		{
			"type": "empty",
		},
	}

	for keyIdx, sessionKey := range sessionKeys {
		for msgIdx, message := range testMessages {
			t.Run(func() string {
				return "key_" + string(rune('0'+keyIdx)) + "_msg_" + string(rune('0'+msgIdx))
			}(), func(t *testing.T) {
				// Encrypt
				encrypted, err := mc.EncryptMessage(message, sessionKey)
				if err != nil {
					t.Fatalf("Encryption failed: %v", err)
				}

				// Decrypt
				decrypted, err := mc.DecryptMessage(encrypted, sessionKey)
				if err != nil {
					t.Fatalf("Decryption failed: %v", err)
				}

				// Convert numbers to float64 for comparison (JSON unmarshaling behavior)
				expectedMessage := convertNumbersToFloat64(message)

				if !reflect.DeepEqual(decrypted, expectedMessage) {
					t.Errorf("Round trip failed.\nOriginal: %v\nDecrypted: %v", expectedMessage, decrypted)
				}
			})
		}
	}
}

// Helper function to convert numbers to float64 (JSON behavior)
func convertNumbersToFloat64(obj interface{}) interface{} {
	switch v := obj.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = convertNumbersToFloat64(val)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = convertNumbersToFloat64(val)
		}
		return result
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	default:
		return v
	}
}

// Benchmark tests
func BenchmarkMessageCryptoEncrypt(b *testing.B) {
	mc := NewMessageCrypto()
	sessionKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	message := map[string]interface{}{
		"type":    "benchmark",
		"content": "This is a benchmark message with some content",
		"number":  42,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mc.EncryptMessage(message, sessionKey)
	}
}

func BenchmarkMessageCryptoDecrypt(b *testing.B) {
	mc := NewMessageCrypto()
	sessionKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	message := map[string]interface{}{
		"type":    "benchmark",
		"content": "This is a benchmark message with some content",
		"number":  42,
	}

	encrypted, _ := mc.EncryptMessage(message, sessionKey)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mc.DecryptMessage(encrypted, sessionKey)
	}
}

func BenchmarkPKCS7Padding(b *testing.B) {
	mc := NewMessageCrypto()
	data := make([]byte, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		padded := mc.applyPKCS7Padding(data, aes.BlockSize)
		mc.removePKCS7Padding(padded)
	}
}

func BenchmarkCreateEncryptedEnvelope(b *testing.B) {
	mc := NewMessageCrypto()
	sessionKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	message := map[string]interface{}{
		"type":    "benchmark",
		"content": "benchmark content",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mc.CreateEncryptedEnvelope(message, sessionKey)
	}
}

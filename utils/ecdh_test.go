package utils

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateECDHKeyPair(t *testing.T) {
	// Clear any existing keys
	ClearECDHKeys()

	t.Run("Valid key generation", func(t *testing.T) {
		err := GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("GenerateECDHKeyPair() failed: %v", err)
		}

		// Verify keys were generated
		publicKey := GetECDHPublicKey()
		if publicKey == "" {
			t.Error("Public key should not be empty after generation")
		}

		// Verify public key is valid base64
		_, err = base64.StdEncoding.DecodeString(publicKey)
		if err != nil {
			t.Errorf("Public key is not valid base64: %v", err)
		}
	})

	t.Run("Multiple generations produce different keys", func(t *testing.T) {
		err := GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("First key generation failed: %v", err)
		}
		firstKey := GetECDHPublicKey()

		err = GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Second key generation failed: %v", err)
		}
		secondKey := GetECDHPublicKey()

		if firstKey == secondKey {
			t.Error("Consecutive key generations should produce different keys")
		}
	})

	t.Run("Concurrent key generation", func(t *testing.T) {
		const numGoroutines = 10
		results := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				results <- GenerateECDHKeyPair()
			}()
		}

		for i := 0; i < numGoroutines; i++ {
			if err := <-results; err != nil {
				t.Errorf("Concurrent key generation failed: %v", err)
			}
		}

		// Verify we still have a valid key
		publicKey := GetECDHPublicKey()
		if publicKey == "" {
			t.Error("No public key available after concurrent generation")
		}
	})
}

func TestGetECDHPublicKey(t *testing.T) {
	t.Run("Empty key before generation", func(t *testing.T) {
		ClearECDHKeys()
		publicKey := GetECDHPublicKey()
		if publicKey != "" {
			t.Error("Public key should be empty before generation")
		}
	})

	t.Run("Valid key after generation", func(t *testing.T) {
		ClearECDHKeys()
		err := GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Key generation failed: %v", err)
		}

		publicKey := GetECDHPublicKey()
		if publicKey == "" {
			t.Error("Public key should not be empty after generation")
		}

		// Verify it's valid base64
		decoded, err := base64.StdEncoding.DecodeString(publicKey)
		if err != nil {
			t.Errorf("Public key is not valid base64: %v", err)
		}

		// P-256 public key should be 65 bytes (uncompressed format)
		if len(decoded) != 65 {
			t.Errorf("Expected public key length 65, got %d", len(decoded))
		}
	})

	t.Run("Concurrent access", func(t *testing.T) {
		ClearECDHKeys()
		err := GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Key generation failed: %v", err)
		}

		const numGoroutines = 50
		results := make(chan string, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				results <- GetECDHPublicKey()
			}()
		}

		firstKey := <-results
		for i := 1; i < numGoroutines; i++ {
			key := <-results
			if key != firstKey {
				t.Error("Concurrent access should return same key")
			}
		}
	})
}

func TestClearECDHKeys(t *testing.T) {
	t.Run("Clear after generation", func(t *testing.T) {
		// Generate keys
		err := GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Key generation failed: %v", err)
		}

		// Verify keys exist
		if GetECDHPublicKey() == "" {
			t.Error("Public key should exist before clearing")
		}
		if GetSessionKey() == "" {
			// Generate session key first
			err = DeriveSharedSecret(GetECDHPublicKey())
			if err == nil {
				err = DeriveSessionKey("test")
				if err == nil && GetSessionKey() == "" {
					t.Error("Session key should exist before clearing")
				}
			}
		}

		// Clear keys
		ClearECDHKeys()

		// Verify keys are cleared
		if GetECDHPublicKey() != "" {
			t.Error("Public key should be empty after clearing")
		}
		if GetSessionKey() != "" {
			t.Error("Session key should be empty after clearing")
		}
	})

	t.Run("Clear empty keys", func(t *testing.T) {
		ClearECDHKeys()
		ClearECDHKeys() // Should not panic
	})

	t.Run("Concurrent clear", func(t *testing.T) {
		err := GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Key generation failed: %v", err)
		}

		const numGoroutines = 10
		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				ClearECDHKeys()
				done <- true
			}()
		}

		for i := 0; i < numGoroutines; i++ {
			<-done
		}

		if GetECDHPublicKey() != "" {
			t.Error("Public key should be empty after concurrent clearing")
		}
	})
}

func TestDeriveSharedSecret(t *testing.T) {
	t.Run("Valid key exchange", func(t *testing.T) {
		// Generate two key pairs (simulate client and server)
		curve := ecdh.P256()
		serverPrivateKey, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate server key: %v", err)
		}
		serverPublicKeyB64 := base64.StdEncoding.EncodeToString(serverPrivateKey.PublicKey().Bytes())

		// Generate client keys
		ClearECDHKeys()
		err = GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Client key generation failed: %v", err)
		}

		// Perform key exchange
		err = DeriveSharedSecret(serverPublicKeyB64)
		if err != nil {
			t.Errorf("DeriveSharedSecret failed: %v", err)
		}
	})

	t.Run("Invalid base64 server key", func(t *testing.T) {
		ClearECDHKeys()
		err := GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Key generation failed: %v", err)
		}

		err = DeriveSharedSecret("invalid-base64!")
		if err == nil {
			t.Error("Expected error for invalid base64 server key")
		}
		if !strings.Contains(err.Error(), "decode") {
			t.Errorf("Expected decode error, got: %v", err)
		}
	})

	t.Run("Invalid server public key", func(t *testing.T) {
		ClearECDHKeys()
		err := GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Key generation failed: %v", err)
		}

		// Valid base64 but invalid key data
		invalidKey := base64.StdEncoding.EncodeToString([]byte("invalid key data"))
		err = DeriveSharedSecret(invalidKey)
		if err == nil {
			t.Error("Expected error for invalid server public key")
		}
	})

	t.Run("No client private key", func(t *testing.T) {
		ClearECDHKeys()

		err := DeriveSharedSecret("YWJjZGVmZ2hpams=") // Valid base64
		if err == nil {
			t.Error("Expected error when no client private key")
		}
		if !strings.Contains(err.Error(), "no client private key") {
			t.Errorf("Expected 'no client private key' error, got: %v", err)
		}
	})
}

func TestDeriveSessionKey(t *testing.T) {
	t.Run("Valid session key derivation", func(t *testing.T) {
		// Setup: Generate keys and shared secret
		curve := ecdh.P256()
		serverPrivateKey, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate server key: %v", err)
		}
		serverPublicKeyB64 := base64.StdEncoding.EncodeToString(serverPrivateKey.PublicKey().Bytes())

		ClearECDHKeys()
		err = GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Client key generation failed: %v", err)
		}

		err = DeriveSharedSecret(serverPublicKeyB64)
		if err != nil {
			t.Fatalf("Shared secret derivation failed: %v", err)
		}

		// Test session key derivation
		err = DeriveSessionKey("test-info")
		if err != nil {
			t.Errorf("DeriveSessionKey failed: %v", err)
		}

		sessionKey := GetSessionKey()
		if sessionKey == "" {
			t.Error("Session key should not be empty")
		}

		// Verify session key is valid base64
		decoded, err := base64.StdEncoding.DecodeString(sessionKey)
		if err != nil {
			t.Errorf("Session key is not valid base64: %v", err)
		}

		// Session key should be 32 bytes (256 bits)
		if len(decoded) != 32 {
			t.Errorf("Expected session key length 32, got %d", len(decoded))
		}
	})

	t.Run("Different info produces different keys", func(t *testing.T) {
		// Setup shared secret
		curve := ecdh.P256()
		serverPrivateKey, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate server key: %v", err)
		}
		serverPublicKeyB64 := base64.StdEncoding.EncodeToString(serverPrivateKey.PublicKey().Bytes())

		ClearECDHKeys()
		err = GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Client key generation failed: %v", err)
		}

		err = DeriveSharedSecret(serverPublicKeyB64)
		if err != nil {
			t.Fatalf("Shared secret derivation failed: %v", err)
		}

		// Derive first session key
		err = DeriveSessionKey("info1")
		if err != nil {
			t.Fatalf("First session key derivation failed: %v", err)
		}
		key1 := GetSessionKey()

		// Derive second session key with different info
		err = DeriveSessionKey("info2")
		if err != nil {
			t.Fatalf("Second session key derivation failed: %v", err)
		}
		key2 := GetSessionKey()

		if key1 == key2 {
			t.Error("Different info strings should produce different session keys")
		}
	})

	t.Run("No shared secret", func(t *testing.T) {
		ClearECDHKeys()
		err := DeriveSessionKey("test-info")
		if err == nil {
			t.Error("Expected error when no shared secret")
		}
		if !strings.Contains(err.Error(), "no shared secret") {
			t.Errorf("Expected 'no shared secret' error, got: %v", err)
		}
	})
}

func TestGetSessionKey(t *testing.T) {
	t.Run("Empty before derivation", func(t *testing.T) {
		ClearECDHKeys()
		sessionKey := GetSessionKey()
		if sessionKey != "" {
			t.Error("Session key should be empty before derivation")
		}
	})

	t.Run("Valid after derivation", func(t *testing.T) {
		// Setup complete key exchange
		curve := ecdh.P256()
		serverPrivateKey, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate server key: %v", err)
		}
		serverPublicKeyB64 := base64.StdEncoding.EncodeToString(serverPrivateKey.PublicKey().Bytes())

		ClearECDHKeys()
		err = GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Client key generation failed: %v", err)
		}

		err = DeriveSharedSecret(serverPublicKeyB64)
		if err != nil {
			t.Fatalf("Shared secret derivation failed: %v", err)
		}

		err = DeriveSessionKey("test-info")
		if err != nil {
			t.Fatalf("Session key derivation failed: %v", err)
		}

		sessionKey := GetSessionKey()
		if sessionKey == "" {
			t.Error("Session key should not be empty after derivation")
		}
	})
}

func TestGetSessionKeyBytes(t *testing.T) {
	t.Run("Nil before derivation", func(t *testing.T) {
		ClearECDHKeys()
		keyBytes := GetSessionKeyBytes()
		if keyBytes != nil {
			t.Error("Session key bytes should be nil before derivation")
		}
	})

	t.Run("Valid after derivation", func(t *testing.T) {
		// Setup complete key exchange
		curve := ecdh.P256()
		serverPrivateKey, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate server key: %v", err)
		}
		serverPublicKeyB64 := base64.StdEncoding.EncodeToString(serverPrivateKey.PublicKey().Bytes())

		ClearECDHKeys()
		err = GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Client key generation failed: %v", err)
		}

		err = DeriveSharedSecret(serverPublicKeyB64)
		if err != nil {
			t.Fatalf("Shared secret derivation failed: %v", err)
		}

		err = DeriveSessionKey("test-info")
		if err != nil {
			t.Fatalf("Session key derivation failed: %v", err)
		}

		keyBytes := GetSessionKeyBytes()
		if keyBytes == nil {
			t.Error("Session key bytes should not be nil after derivation")
		}

		if len(keyBytes) != 32 {
			t.Errorf("Expected session key bytes length 32, got %d", len(keyBytes))
		}
	})

	t.Run("Returns copy not reference", func(t *testing.T) {
		// Setup session key
		curve := ecdh.P256()
		serverPrivateKey, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate server key: %v", err)
		}
		serverPublicKeyB64 := base64.StdEncoding.EncodeToString(serverPrivateKey.PublicKey().Bytes())

		ClearECDHKeys()
		err = GenerateECDHKeyPair()
		if err != nil {
			t.Fatalf("Client key generation failed: %v", err)
		}

		err = DeriveSharedSecret(serverPublicKeyB64)
		if err != nil {
			t.Fatalf("Shared secret derivation failed: %v", err)
		}

		err = DeriveSessionKey("test-info")
		if err != nil {
			t.Fatalf("Session key derivation failed: %v", err)
		}

		keyBytes1 := GetSessionKeyBytes()
		keyBytes2 := GetSessionKeyBytes()

		// Modify first copy
		if len(keyBytes1) > 0 {
			keyBytes1[0] = 0xFF
		}

		// Second copy should be unchanged
		if len(keyBytes2) > 0 && keyBytes2[0] == 0xFF {
			t.Error("GetSessionKeyBytes should return a copy, not a reference")
		}
	})
}

// Benchmark tests
func BenchmarkGenerateECDHKeyPair(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateECDHKeyPair()
	}
}

func BenchmarkGetECDHPublicKey(b *testing.B) {
	GenerateECDHKeyPair()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetECDHPublicKey()
	}
}

func BenchmarkDeriveSharedSecret(b *testing.B) {
	// Setup
	curve := ecdh.P256()
	serverPrivateKey, _ := curve.GenerateKey(rand.Reader)
	serverPublicKeyB64 := base64.StdEncoding.EncodeToString(serverPrivateKey.PublicKey().Bytes())

	GenerateECDHKeyPair()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		DeriveSharedSecret(serverPublicKeyB64)
	}
}

func BenchmarkDeriveSessionKey(b *testing.B) {
	// Setup
	curve := ecdh.P256()
	serverPrivateKey, _ := curve.GenerateKey(rand.Reader)
	serverPublicKeyB64 := base64.StdEncoding.EncodeToString(serverPrivateKey.PublicKey().Bytes())

	GenerateECDHKeyPair()
	DeriveSharedSecret(serverPublicKeyB64)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		DeriveSessionKey("test-info")
	}
}

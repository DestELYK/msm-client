package utils

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"

	"golang.org/x/crypto/hkdf"
)

// ECDH key management
var (
	ecdhPrivateKey *ecdh.PrivateKey
	ecdhPublicKey  []byte
	sharedSecret   []byte
	sessionKey     []byte
	ecdhMutex      sync.RWMutex
)

// GenerateECDHKeyPair generates a new ECDH key pair and stores it
func GenerateECDHKeyPair() error {
	ecdhMutex.Lock()
	defer ecdhMutex.Unlock()

	// Use P-256 curve for ECDH
	curve := ecdh.P256()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ECDH private key: %w", err)
	}

	publicKey := privateKey.PublicKey().Bytes()

	ecdhPrivateKey = privateKey
	ecdhPublicKey = publicKey

	return nil
}

// GetECDHPublicKey returns the current ECDH public key (base64 encoded)
func GetECDHPublicKey() string {
	ecdhMutex.RLock()
	defer ecdhMutex.RUnlock()

	if len(ecdhPublicKey) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(ecdhPublicKey)
}

// ClearECDHKeys clears the stored ECDH keys
func ClearECDHKeys() {
	ecdhMutex.Lock()
	defer ecdhMutex.Unlock()

	ecdhPrivateKey = nil
	ecdhPublicKey = nil
	sharedSecret = nil
	sessionKey = nil
}

// DeriveSharedSecret performs ECDH key exchange with server's public key
func DeriveSharedSecret(serverPublicKeyB64 string) error {
	ecdhMutex.Lock()
	defer ecdhMutex.Unlock()

	if ecdhPrivateKey == nil {
		return fmt.Errorf("no client private key available")
	}

	// Decode server's public key from base64
	serverPublicKeyBytes, err := base64.StdEncoding.DecodeString(serverPublicKeyB64)
	if err != nil {
		return fmt.Errorf("failed to decode server public key: %w", err)
	}

	// Parse server's public key
	curve := ecdh.P256()
	serverPublicKey, err := curve.NewPublicKey(serverPublicKeyBytes)
	if err != nil {
		return fmt.Errorf("failed to parse server public key: %w", err)
	}

	// Perform ECDH key exchange
	sharedSecret, err = ecdhPrivateKey.ECDH(serverPublicKey)
	if err != nil {
		return fmt.Errorf("ECDH key exchange failed: %w", err)
	}

	return nil
}

// DeriveSessionKey derives a session key from the shared secret using HKDF
func DeriveSessionKey(info string) error {
	ecdhMutex.Lock()
	defer ecdhMutex.Unlock()

	if sharedSecret == nil {
		return fmt.Errorf("no shared secret available")
	}

	// Use HKDF to derive a 32-byte session key
	hkdf := hkdf.New(sha256.New, sharedSecret, nil, []byte(info))
	sessionKey = make([]byte, 32)
	if _, err := hkdf.Read(sessionKey); err != nil {
		return fmt.Errorf("failed to derive session key: %w", err)
	}

	return nil
}

// GetSessionKey returns the current session key (base64 encoded)
func GetSessionKey() string {
	ecdhMutex.RLock()
	defer ecdhMutex.RUnlock()

	if len(sessionKey) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(sessionKey)
}

// GetSessionKeyBytes returns the current session key as bytes (for encryption/decryption)
func GetSessionKeyBytes() []byte {
	ecdhMutex.RLock()
	defer ecdhMutex.RUnlock()

	if len(sessionKey) == 0 {
		return nil
	}

	// Return a copy to prevent external modification
	result := make([]byte, len(sessionKey))
	copy(result, sessionKey)
	return result
}

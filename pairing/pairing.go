package pairing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"msm-client/config"
	"msm-client/state"
	"msm-client/utils"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// Pairing code management
	pairCode  string
	expiry    time.Time
	failCount int
	codeMutex sync.Mutex

	// Server management
	pairServer    *http.Server
	cancelCleanup context.CancelFunc
	serverMutex   sync.RWMutex
	serverRunning bool

	// Global configuration
	globalConfig config.ClientConfig
	configMutex  sync.RWMutex

	// Test mode
	TestMode bool

	// Callback functions for external use
	onPairingStarted func(code string, expiry time.Time)
	onPairingSuccess func(serverWs, token string)
	onPairingFailed  func(reason string, failCount int)
	onServerStarted  func(addr string)
	onServerStopped  func()
	callbackMutex    sync.RWMutex
)

const pairingCodeFile = "pairing_code.txt"
const maxFailCount = 3
const pairingCodeCleanupInterval = 5 * time.Second
const pairingCodeExpiry = 5 * time.Minute
const pairingCodeLength = 6

// Global server management functions

// IsServerRunning returns whether the pairing server is currently running
func IsServerRunning() bool {
	serverMutex.RLock()
	defer serverMutex.RUnlock()
	return serverRunning && pairServer != nil
}

// GetServer returns the current pairing server instance
func GetServer() *http.Server {
	serverMutex.RLock()
	defer serverMutex.RUnlock()
	return pairServer
}

// SetConfig sets the global configuration for pairing operations
func SetConfig(cfg config.ClientConfig) {
	configMutex.Lock()
	defer configMutex.Unlock()
	globalConfig = cfg
}

// GetConfig returns the current global configuration
func GetConfig() config.ClientConfig {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return globalConfig
}

// setServer sets the pairing server instance (internal use)
func setServer(server *http.Server) {
	serverMutex.Lock()
	defer serverMutex.Unlock()
	pairServer = server
	serverRunning = (server != nil)
}

// clearServer clears the pairing server instance (internal use)
func clearServer() {
	serverMutex.Lock()
	defer serverMutex.Unlock()
	pairServer = nil
	serverRunning = false
}

// Callback management functions

// SetOnPairingStarted sets a callback for when pairing code is generated
func SetOnPairingStarted(callback func(code string, expiry time.Time)) {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	onPairingStarted = callback
}

// SetOnPairingSuccess sets a callback for successful pairing
func SetOnPairingSuccess(callback func(serverWs, token string)) {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	onPairingSuccess = callback
}

// SetOnPairingFailed sets a callback for failed pairing attempts
func SetOnPairingFailed(callback func(reason string, failCount int)) {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	onPairingFailed = callback
}

// SetOnServerStarted sets a callback for when the server starts
func SetOnServerStarted(callback func(addr string)) {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	onServerStarted = callback
}

// SetOnServerStopped sets a callback for when the server stops
func SetOnServerStopped(callback func()) {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	onServerStopped = callback
}

// ClearAllCallbacks clears all callback functions
func ClearAllCallbacks() {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	onPairingStarted = nil
	onPairingSuccess = nil
	onPairingFailed = nil
	onServerStarted = nil
	onServerStopped = nil
}

// triggerCallback safely calls a callback function
func triggerOnPairingStarted(code string, expiry time.Time) {
	callbackMutex.RLock()
	callback := onPairingStarted
	callbackMutex.RUnlock()
	if callback != nil {
		callback(code, expiry)
	}
}

func triggerOnPairingSuccess(serverWs, token string) {
	callbackMutex.RLock()
	callback := onPairingSuccess
	callbackMutex.RUnlock()
	if callback != nil {
		callback(serverWs, token)
	}
}

func triggerOnPairingFailed(reason string, failCount int) {
	callbackMutex.RLock()
	callback := onPairingFailed
	callbackMutex.RUnlock()
	if callback != nil {
		callback(reason, failCount)
	}
}

func triggerOnServerStarted(addr string) {
	callbackMutex.RLock()
	callback := onServerStarted
	callbackMutex.RUnlock()
	if callback != nil {
		callback(addr)
	}
}

func triggerOnServerStopped() {
	callbackMutex.RLock()
	callback := onServerStopped
	callbackMutex.RUnlock()
	if callback != nil {
		callback()
	}
}

func StartPairingServer(cfg config.ClientConfig) {
	StartPairingServerOnPort(cfg, 6969)
}

// StartPairingServerOnPort starts the pairing server on a specific port (for testing)
func StartPairingServerOnPort(cfg config.ClientConfig, port int) {
	// Set global configuration first (even in test mode)
	SetConfig(cfg)

	// Check if we're in test mode and should skip server operations
	if TestMode {
		log.Println("Test mode: Skipping actual pairing server start")
		return
	}

	// Check if server is already running
	if IsServerRunning() {
		log.Println("Pairing server already running")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/pair", HandlePair(cfg))
	mux.HandleFunc("/pair/confirm", HandleConfirm(cfg))

	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Set the global server instance
	setServer(server)

	// Start the cleanup goroutine when server starts
	ctx, cancel := context.WithCancel(context.Background())
	cancelCleanup = cancel

	go func() {
		ticker := time.NewTicker(pairingCodeCleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				codeMutex.Lock()
				if time.Now().After(expiry) && pairCode != "" {
					log.Printf("Pairing code '%s' expired, invalidating code (had %d failed attempts)", pairCode, failCount)
					pairCode = ""
					expiry = time.Time{}
					failCount = 0
					_ = DeletePairingCode()
				} else if failCount >= maxFailCount && pairCode != "" {
					log.Printf("Max pairing attempts reached for code '%s' (%d/%d failed attempts), invalidating code", pairCode, failCount, maxFailCount)
					pairCode = ""
					expiry = time.Time{}
					failCount = 0
					_ = DeletePairingCode()
				}
				codeMutex.Unlock()
			case <-ctx.Done():
				log.Println("Pairing cleanup stopped")
				return
			}
		}
	}()

	log.Printf("Pairing server started on %s", addr)

	// Trigger server started callback
	triggerOnServerStarted(addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("Pairing server failed: %v", err)
		clearServer()
		triggerOnServerStopped()
		return
	}
	log.Println("Pairing server stopped")
	clearServer()
	triggerOnServerStopped()
}

func HandlePair(cfg config.ClientConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		codeMutex.Lock()
		defer codeMutex.Unlock()

		pairCode = GenerateCode()
		expiry = time.Now().Add(pairingCodeExpiry)
		failCount = 0

		log.Printf("Generated pairing code: %s, expires at %s", pairCode, expiry.Local().Format(time.RFC3339))

		SavePairingCode(pairCode)

		// Trigger pairing started callback
		triggerOnPairingStarted(pairCode, expiry)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "Pairing code generated, please check device for code.",
			"expiry":  expiry.Format(time.RFC3339),
		})
	}
}

func HandleConfirm(cfg config.ClientConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code     string `json:"code"`
			ServerWs string `json:"serverWs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Pairing attempt failed: invalid request format")
			triggerOnPairingFailed("invalid_request", failCount)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		codeMutex.Lock()
		defer codeMutex.Unlock()

		log.Printf("Pairing attempt received: code='%s' (attempt %d/%d)", req.Code, failCount+1, maxFailCount)

		if time.Now().After(expiry) || failCount >= 3 {
			log.Printf("Pairing attempt rejected: code expired or max attempts reached (failCount: %d)", failCount)
			triggerOnPairingFailed("expired_or_max_attempts", failCount)
			http.Error(w, "Code expired or max attempts", http.StatusForbidden)
			return
		}
		if req.Code != pairCode {
			failCount++
			log.Printf("Pairing attempt failed: incorrect code '%s' (expected '%s'). Fail count: %d/%d", req.Code, pairCode, failCount, maxFailCount)
			triggerOnPairingFailed("incorrect_code", failCount)
			http.Error(w, "Incorrect code", http.StatusUnauthorized)
			return
		}

		log.Printf("Pairing successful! Code '%s' accepted. Connecting to %s", req.Code, req.ServerWs)
		token := CreateJWT(cfg.ClientID)
		state.SaveState(state.PairedState{ServerWs: req.ServerWs, Token: token})

		DeletePairingCode() // Clear pairing code after successful pairing

		// Trigger success callback
		triggerOnPairingSuccess(req.ServerWs, token)

		// Cancel the cleanup goroutine since pairing was successful
		if cancelCleanup != nil {
			cancelCleanup()
			cancelCleanup = nil
		}

		// Get all network interfaces (Ethernet and WiFi only)
		networkInterfaces := utils.GetNetworkInterfaces()

		responseData := map[string]any{
			"message":    "paired",
			"token":      token,
			"clientId":   cfg.ClientID,
			"interfaces": networkInterfaces,
		}

		_ = json.NewEncoder(w).Encode(responseData)

		go func() {
			// Give the HTTP response time to be sent
			time.Sleep(100 * time.Millisecond)
			server := GetServer()
			if server != nil {
				_ = server.Shutdown(context.Background())
			}
		}()
	}
}

func GenerateCode() string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, pairingCodeLength)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func CreateJWT(clientID string) string {
	secret_key := config.GetSecretKey()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"clientId": clientID,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	})
	signed, _ := token.SignedString([]byte(secret_key))
	return signed
}

func ValidateCode(code string) bool {
	codeMutex.Lock()
	defer codeMutex.Unlock()

	if time.Now().After(expiry) || failCount >= 3 {
		return false
	}
	if code != pairCode {
		failCount++
		return false
	}
	return true
}

func ResetPairing() {
	codeMutex.Lock()
	defer codeMutex.Unlock()

	pairCode = ""
	expiry = time.Time{}
	failCount = 0
	log.Println("Pairing reset")

	DeletePairingCode()
}

func StopPairingServer() {
	server := GetServer()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down pairing server: %v", err)
		} else {
			log.Println("Pairing server stopped gracefully")
		}
		clearServer()
		triggerOnServerStopped()
	}

	// Cancel cleanup goroutine if running
	if cancelCleanup != nil {
		cancelCleanup()
		cancelCleanup = nil
	}
}

func GetPairingCode() (string, time.Time) {
	codeMutex.Lock()
	defer codeMutex.Unlock()

	if time.Now().After(expiry) || failCount >= 3 {
		return "", time.Time{}
	}
	return pairCode, expiry
}

func GetPairingStatus() (string, time.Time, int) {
	codeMutex.Lock()
	defer codeMutex.Unlock()

	if time.Now().After(expiry) || failCount >= 3 {
		return "expired", time.Time{}, failCount
	}
	return pairCode, expiry, failCount
}

func WatchPairingCode(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastCode := ""

	// Check immediately on start
	checkCode := func() {
		code, err := LoadPairingCode()
		if err != nil {
			log.Printf("Failed to load pairing code")
		}

		if code != lastCode {
			lastCode = code
			if code == "" {
				log.Println("Pairing code cleared or expired")
				return
			}

			log.Printf("Current pairing code: %s", code)
		}
	}

	// Initial check
	checkCode()

	// Then check every interval
	for range ticker.C {
		checkCode()
	}
}

func SavePairingCode(code string) error {
	return utils.WriteFile(pairingCodeFile, []byte(code))
}

func LoadPairingCode() (string, error) {
	data, err := utils.ReadFile(pairingCodeFile)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func DeletePairingCode() error {
	return utils.DeleteFile(pairingCodeFile)
}

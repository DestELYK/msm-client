package pairing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"msm-client/config"
	"msm-client/state"
	"msm-client/utils"
)

// PairingManager handles all pairing operations
type PairingManager struct {
	// Pairing code management
	pairCode   string
	expiry     time.Time
	failCount  int
	pairCodeIP string // IP address that generated the current pairing code
	codeMutex  sync.Mutex

	// IP blacklist management
	ipBlacklist    map[string]time.Time // IP -> blacklist expiry time
	ipViolations   map[string]int       // IP -> violation count
	blacklistMutex sync.Mutex

	// Server management
	pairServer     *http.Server
	cancelCleanup  context.CancelFunc
	serverMutex    sync.RWMutex
	serverRunning  bool
	showingDisplay bool

	// Global configuration
	globalConfig config.ClientConfig
	configMutex  sync.RWMutex

	// Callback functions for external use
	onPairingStarted func(code string, expiry time.Time)
	onPairingSuccess func(serverWs string)
	onPairingFailed  func(reason string, failCount int)
	onServerStarted  func(addr string)
	onServerStopped  func()
	callbackMutex    sync.RWMutex

	// Display manager
	display *PairingDisplay
}

const DEFAULT_PATH = "/var/lib/msm-client"   // Default path for pairing file
const PAIRING_CODE_FILE = "pairing_code.txt" // File name for pairing code
const MAX_FAIL_COUNT = 3                     // Max pairing attempts before invalidating code

// IP security constants
const MAX_IP_VIOLATIONS = 3                 // Max IP violations before blacklisting
const IP_BLACKLIST_DURATION = 1 * time.Hour // How long to blacklist an IP

// NewPairingManager creates a new PairingManager instance
func NewPairingManager() *PairingManager {
	pm := &PairingManager{
		ipBlacklist:  make(map[string]time.Time),
		ipViolations: make(map[string]int),
	}

	// Initialize the display manager
	pm.display = NewPairingDisplay(pm)

	return pm
}

// getClientIP extracts the real client IP from the HTTP request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// Fall back to remote address
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isIPBlacklisted checks if an IP is currently blacklisted
func (pm *PairingManager) isIPBlacklisted(ip string) bool {
	pm.blacklistMutex.Lock()
	defer pm.blacklistMutex.Unlock()

	if expiry, exists := pm.ipBlacklist[ip]; exists {
		if time.Now().Before(expiry) {
			return true
		}
		// Clean up expired blacklist entry
		delete(pm.ipBlacklist, ip)
	}
	return false
}

// recordIPViolation records a violation for an IP and blacklists if necessary
func (pm *PairingManager) recordIPViolation(ip string) bool {
	pm.blacklistMutex.Lock()
	defer pm.blacklistMutex.Unlock()

	// Increment violation count
	pm.ipViolations[ip]++
	violations := pm.ipViolations[ip]

	log.Printf("IP violation recorded for %s: %d/%d violations", ip, violations, MAX_IP_VIOLATIONS)

	// Blacklist if max violations reached
	if violations >= MAX_IP_VIOLATIONS {
		pm.ipBlacklist[ip] = time.Now().Add(IP_BLACKLIST_DURATION)
		log.Printf("IP %s blacklisted for %v due to %d violations", ip, IP_BLACKLIST_DURATION, violations)
		return true
	}

	return false
}

// cleanupBlacklist removes expired blacklist entries
func (pm *PairingManager) cleanupBlacklist() {
	pm.blacklistMutex.Lock()
	defer pm.blacklistMutex.Unlock()

	now := time.Now()
	for ip, expiry := range pm.ipBlacklist {
		if now.After(expiry) {
			delete(pm.ipBlacklist, ip)
			delete(pm.ipViolations, ip) // Also reset violation count
			log.Printf("Removed expired blacklist entry for IP %s", ip)
		}
	}
}

// getPairingPath returns the path for the pairing code file based on environment variable or default
func getPairingPath() string {
	if path := os.Getenv("MSC_PAIRING_PATH"); path != "" {
		return filepath.Join(path, PAIRING_CODE_FILE)
	}
	return filepath.Join(DEFAULT_PATH, PAIRING_CODE_FILE)
}

const pairingCodeCleanupInterval = 5 * time.Second
const pairingCodeExpiry = 1 * time.Minute
const pairingCodeLength = 6

// IsServerRunning returns whether the pairing server is currently running
func (pm *PairingManager) IsServerRunning() bool {
	pm.serverMutex.RLock()
	defer pm.serverMutex.RUnlock()
	return pm.serverRunning && pm.pairServer != nil
}

// GetServer returns the current pairing server instance
func (pm *PairingManager) GetServer() *http.Server {
	pm.serverMutex.RLock()
	defer pm.serverMutex.RUnlock()
	return pm.pairServer
}

// SetConfig sets the global configuration for pairing operations
func (pm *PairingManager) SetConfig(cfg config.ClientConfig) {
	pm.configMutex.Lock()
	defer pm.configMutex.Unlock()
	pm.globalConfig = cfg
}

// GetConfig returns the current global configuration
func (pm *PairingManager) GetConfig() config.ClientConfig {
	pm.configMutex.RLock()
	defer pm.configMutex.RUnlock()
	return pm.globalConfig
}

// setServer sets the pairing server instance (internal use)
func (pm *PairingManager) setServer(server *http.Server) {
	pm.serverMutex.Lock()
	defer pm.serverMutex.Unlock()
	pm.pairServer = server
	pm.serverRunning = (server != nil)
}

// clearServer clears the pairing server instance (internal use)
func (pm *PairingManager) clearServer() {
	pm.serverMutex.Lock()
	defer pm.serverMutex.Unlock()
	pm.pairServer = nil
	pm.serverRunning = false
}

// Callback management functions

// SetOnPairingStarted sets a callback for when pairing code is generated
func (pm *PairingManager) SetOnPairingStarted(callback func(code string, expiry time.Time)) {
	pm.callbackMutex.Lock()
	defer pm.callbackMutex.Unlock()
	pm.onPairingStarted = callback
}

// SetOnPairingSuccess sets a callback for successful pairing
func (pm *PairingManager) SetOnPairingSuccess(callback func(serverWs string)) {
	pm.callbackMutex.Lock()
	defer pm.callbackMutex.Unlock()
	pm.onPairingSuccess = callback
}

// SetOnPairingFailed sets a callback for failed pairing attempts
func (pm *PairingManager) SetOnPairingFailed(callback func(reason string, failCount int)) {
	pm.callbackMutex.Lock()
	defer pm.callbackMutex.Unlock()
	pm.onPairingFailed = callback
}

// SetOnServerStarted sets a callback for when the server starts
func (pm *PairingManager) SetOnServerStarted(callback func(addr string)) {
	pm.callbackMutex.Lock()
	defer pm.callbackMutex.Unlock()
	pm.onServerStarted = callback
}

// SetOnServerStopped sets a callback for when the server stops
func (pm *PairingManager) SetOnServerStopped(callback func()) {
	pm.callbackMutex.Lock()
	defer pm.callbackMutex.Unlock()
	pm.onServerStopped = callback
}

// ClearAllCallbacks clears all callback functions
func (pm *PairingManager) ClearAllCallbacks() {
	pm.callbackMutex.Lock()
	defer pm.callbackMutex.Unlock()
	pm.onPairingStarted = nil
	pm.onPairingSuccess = nil
	pm.onPairingFailed = nil
	pm.onServerStarted = nil
	pm.onServerStopped = nil
}

// triggerCallback safely calls a callback function
func (pm *PairingManager) triggerOnPairingStarted(code string, expiry time.Time) {
	pm.callbackMutex.RLock()
	callback := pm.onPairingStarted
	pm.callbackMutex.RUnlock()
	if callback != nil {
		callback(code, expiry)
	}
}

func (pm *PairingManager) triggerOnPairingSuccess(serverWs string) {
	pm.callbackMutex.RLock()
	callback := pm.onPairingSuccess
	pm.callbackMutex.RUnlock()
	if callback != nil {
		callback(serverWs)
	}
}

func (pm *PairingManager) triggerOnPairingFailed(reason string, failCount int) {
	pm.callbackMutex.RLock()
	callback := pm.onPairingFailed
	pm.callbackMutex.RUnlock()
	if callback != nil {
		callback(reason, failCount)
	}
}

func (pm *PairingManager) triggerOnServerStarted(addr string) {
	pm.callbackMutex.RLock()
	callback := pm.onServerStarted
	pm.callbackMutex.RUnlock()
	if callback != nil {
		callback(addr)
	}
}

func (pm *PairingManager) triggerOnServerStopped() {
	pm.callbackMutex.RLock()
	callback := pm.onServerStopped
	pm.callbackMutex.RUnlock()
	if callback != nil {
		callback()
	}
}

// StartPairingServerOnPort starts the pairing server on a specific port using the manager
func (pm *PairingManager) StartPairingServerOnPort(cfg config.ClientConfig, port int, enableDisplay bool) {
	// Set global configuration first (even in test mode)
	pm.SetConfig(cfg)

	pm.showingDisplay = enableDisplay

	// Check if server is already running
	if pm.IsServerRunning() {
		log.Println("Pairing server already running")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/pair", pm.HandlePair(cfg))
	mux.HandleFunc("/pair/confirm", pm.HandleConfirm(cfg))

	// Add pairing display route if enabled
	if enableDisplay {
		mux.HandleFunc("/display", pm.display.HandleQRCodeDisplay(cfg))
	}

	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Set the global server instance
	pm.setServer(server)

	// Start the cleanup goroutine when server starts
	ctx, cancel := context.WithCancel(context.Background())
	pm.cancelCleanup = cancel

	go func() {
		ticker := time.NewTicker(pairingCodeCleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Clean up expired pairing codes
				pm.codeMutex.Lock()
				if time.Now().After(pm.expiry) && pm.pairCode != "" {
					log.Printf("Pairing code '%s' expired, invalidating code (had %d failed attempts)", pm.pairCode, pm.failCount)
					pm.pairCode = ""
					pm.pairCodeIP = ""
					pm.expiry = time.Time{}
					pm.failCount = 0
					_ = pm.DeletePairingCode()
					utils.ClearECDHKeys() // Clear ECDH keys when code expires
				} else if pm.failCount >= MAX_FAIL_COUNT && pm.pairCode != "" {
					log.Printf("Max pairing attempts reached for code '%s' (%d/%d failed attempts), invalidating code", pm.pairCode, pm.failCount, MAX_FAIL_COUNT)
					pm.pairCode = ""
					pm.pairCodeIP = ""
					pm.expiry = time.Time{}
					pm.failCount = 0
					_ = pm.DeletePairingCode()
					utils.ClearECDHKeys() // Clear ECDH keys when max attempts reached
				}
				pm.codeMutex.Unlock()

				// Clean up expired blacklist entries
				pm.cleanupBlacklist()
			case <-ctx.Done():
				log.Println("Pairing cleanup stopped")
				return
			}
		}
	}()

	log.Printf("Pairing server started on %s", addr)

	// Trigger server started callback
	pm.triggerOnServerStarted(addr)

	// Start server in a goroutine to avoid blocking
	serverDone := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Pairing server failed: %v", err)
			serverDone <- err
		} else {
			log.Println("Pairing server stopped")
			serverDone <- nil
		}
	}()

	// Wait for server to finish
	<-serverDone
	pm.clearServer()
	pm.triggerOnServerStopped()
}

func (pm *PairingManager) HandlePair(cfg config.ClientConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		clientIP := getClientIP(r)

		// Check if IP is blacklisted
		if pm.isIPBlacklisted(clientIP) {
			log.Printf("Pairing request rejected: IP %s is blacklisted", clientIP)
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		pm.codeMutex.Lock()
		defer pm.codeMutex.Unlock()

		// Check if a valid pairing code already exists
		if pm.pairCode != "" && time.Now().Before(pm.expiry) {
			log.Printf("Pairing code request from IP %s: existing valid code '%s' still active, expires at %s", clientIP, pm.pairCode, pm.expiry.Local().Format(time.RFC3339))

			// Return the existing code information
			message := "Pairing code already active, "
			if pm.showingDisplay {
				message += "please check /display for the code."
			} else {
				message += "please check device for code."
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": message,
				"expiry":  pm.expiry.Format(time.RFC3339),
			})
			return
		}

		// Generate new code only if no valid code exists
		pm.pairCode = utils.GenerateCode(pairingCodeLength)
		pm.pairCodeIP = clientIP
		pm.expiry = time.Now().Add(pairingCodeExpiry)
		pm.failCount = 0

		// Generate ECDH key pair for secure communication
		log.Printf("Generating ECDH key pair for pairing session...")
		if err := utils.GenerateECDHKeyPair(); err != nil {
			log.Printf("Failed to generate ECDH key pair: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		log.Printf("ECDH key pair generated successfully")

		log.Printf("Generated pairing code: %s for IP %s, expires at %s", pm.pairCode, clientIP, pm.expiry.Local().Format(time.RFC3339))

		pm.SavePairingCode(pm.pairCode)

		// Trigger pairing started callback
		pm.triggerOnPairingStarted(pm.pairCode, pm.expiry)

		message := "Pairing code generated, "

		if pm.showingDisplay {
			message += "please check /display for the code."
		} else {
			message += "please check device for code."
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": message,
			"expiry":  pm.expiry.Format(time.RFC3339),
		})
	}
}

func (pm *PairingManager) HandleConfirm(cfg config.ClientConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		clientIP := getClientIP(r)

		// Check if IP is blacklisted
		if pm.isIPBlacklisted(clientIP) {
			log.Printf("Pairing confirmation rejected: IP %s is blacklisted", clientIP)
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		var req struct {
			Code            string `json:"code"`
			ServerWs        string `json:"serverWs"`
			ServerPublicKey string `json:"serverPublicKey"` // Server's ECDH public key (base64)
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Pairing attempt failed: invalid request format from IP %s", clientIP)
			pm.triggerOnPairingFailed("invalid_request", pm.failCount)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		pm.codeMutex.Lock()
		defer pm.codeMutex.Unlock()

		log.Printf("Pairing attempt received from IP %s: code='%s' (attempt %d/%d)", clientIP, req.Code, pm.failCount+1, MAX_FAIL_COUNT)

		// Check if IP matches the one that generated the code
		if pm.pairCodeIP != "" && clientIP != pm.pairCodeIP {
			log.Printf("Pairing attempt rejected: IP mismatch. Code generated by %s, attempt from %s", pm.pairCodeIP, clientIP)

			// Record violation and potentially blacklist the IP
			pm.recordIPViolation(clientIP)

			pm.triggerOnPairingFailed("ip_mismatch", pm.failCount)
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		if time.Now().After(pm.expiry) || pm.failCount >= 3 {
			log.Printf("Pairing attempt rejected: code expired or max attempts reached (failCount: %d)", pm.failCount)
			pm.triggerOnPairingFailed("expired_or_max_attempts", pm.failCount)
			http.Error(w, "Code expired or max attempts", http.StatusForbidden)
			return
		}
		if req.Code != pm.pairCode {
			pm.failCount++
			log.Printf("Pairing attempt failed: incorrect code '%s' (expected '%s'). Fail count: %d/%d", req.Code, pm.pairCode, pm.failCount, MAX_FAIL_COUNT)
			pm.triggerOnPairingFailed("incorrect_code", pm.failCount)
			http.Error(w, "Incorrect code", http.StatusUnauthorized)
			return
		}

		log.Printf("Pairing successful! Code '%s' accepted from IP %s. Connecting to %s", req.Code, clientIP, req.ServerWs)

		// Perform ECDH key exchange if server public key is provided
		var sessionKeyB64 string
		if req.ServerPublicKey != "" {
			log.Printf("Server provided public key, performing ECDH key exchange...")
			// Derive shared secret using ECDH
			if err := utils.DeriveSharedSecret(req.ServerPublicKey); err != nil {
				log.Printf("Failed to derive shared secret: %v", err)
				http.Error(w, "Key exchange failed", http.StatusInternalServerError)
				return
			}

			// Derive session key using HKDF with pairing code as info
			keyInfo := fmt.Sprintf("msm-pairing-%s", req.Code)
			if err := utils.DeriveSessionKey(keyInfo); err != nil {
				log.Printf("Failed to derive session key: %v", err)
				http.Error(w, "Key derivation failed", http.StatusInternalServerError)
				return
			}

			// Get the derived session key
			sessionKeyB64 = utils.GetSessionKey()
			if sessionKeyB64 == "" {
				log.Printf("Session key derivation succeeded but key is empty")
				http.Error(w, "Session key invalid", http.StatusInternalServerError)
				return
			}

			log.Printf("Successfully completed ECDH key exchange and derived session key")
		} else {
			log.Printf("No server public key provided, skipping ECDH key exchange")
		}

		// Save the pairing state with session key if available
		pairedState := state.PairedState{
			ServerWs:   req.ServerWs,
			SessionKey: sessionKeyB64, // Will be empty string if no ECDH was performed
		}
		state.SaveState(pairedState)

		// Trigger success callback
		pm.triggerOnPairingSuccess(req.ServerWs)

		// Cancel the cleanup goroutine since pairing was successful
		if pm.cancelCleanup != nil {
			pm.cancelCleanup()
			pm.cancelCleanup = nil
		}

		// Get all network interfaces (Ethernet and WiFi only)
		interfaces := utils.GetNetworkInterfaces()
		networkInterfaces := make([]any, len(interfaces))
		for i, iface := range interfaces {
			networkInterfaces[i] = iface
		}

		// Get the ECDH public key for response
		log.Printf("Getting ECDH public key for response...")
		ecdhPublicKeyB64 := utils.GetECDHPublicKey()
		log.Printf("ECDH public key length: %d, session key available: %t", len(ecdhPublicKeyB64), sessionKeyB64 != "")

		// Ensure we have a valid client ID
		clientId := "unknown"
		if cfg.ClientID != "" {
			clientId = cfg.ClientID
		}

		responseData := map[string]any{
			"message":       "paired",
			"clientId":      clientId,
			"interfaces":    networkInterfaces,
			"ecdhPublicKey": ecdhPublicKeyB64,
		}

		// Include session key in response if available (for verification/debugging)
		if sessionKeyB64 != "" {
			responseData["sessionKeyDerived"] = true
			log.Printf("Session key successfully derived and ready for secure communication")
		}

		// Clear ECDH keys after constructing response
		utils.ClearECDHKeys()

		_ = json.NewEncoder(w).Encode(responseData)

		go func() {
			// Give the HTTP response time to be sent
			time.Sleep(100 * time.Millisecond)
			server := pm.GetServer()
			if server != nil {
				_ = server.Shutdown(context.Background())
			}
			pm.ResetPairing()
		}()
	}
}

func (pm *PairingManager) ValidateCode(code string) bool {
	pm.codeMutex.Lock()
	defer pm.codeMutex.Unlock()

	if time.Now().After(pm.expiry) || pm.failCount >= 3 {
		return false
	}
	if code != pm.pairCode {
		pm.failCount++
		return false
	}
	return true
}

func (pm *PairingManager) ResetPairing() {
	pm.codeMutex.Lock()
	defer pm.codeMutex.Unlock()

	pm.pairCode = ""
	pm.pairCodeIP = ""
	pm.expiry = time.Time{}
	pm.failCount = 0
	log.Println("Pairing reset")

	pm.DeletePairingCode()
}

func (pm *PairingManager) StopPairingServer() {
	server := pm.GetServer()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down pairing server: %v", err)
		} else {
			log.Println("Pairing server stopped gracefully")
		}
		pm.clearServer()
		pm.triggerOnServerStopped()
		pm.ResetPairing()
	}

	// Cancel cleanup goroutine if running
	if pm.cancelCleanup != nil {
		pm.cancelCleanup()
		pm.cancelCleanup = nil
	}

	// Clear ECDH keys when server stops
	utils.ClearECDHKeys()
}

func (pm *PairingManager) GetPairingCode() (string, time.Time) {
	pm.codeMutex.Lock()
	defer pm.codeMutex.Unlock()

	if time.Now().After(pm.expiry) || pm.failCount >= 3 {
		return "", time.Time{}
	}
	return pm.pairCode, pm.expiry
}

func (pm *PairingManager) GetPairingStatus() (string, time.Time, int) {
	pm.codeMutex.Lock()
	defer pm.codeMutex.Unlock()

	if time.Now().After(pm.expiry) || pm.failCount >= 3 {
		return "expired", time.Time{}, pm.failCount
	}
	return pm.pairCode, pm.expiry, pm.failCount
}

func (pm *PairingManager) WatchPairingCode(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastCode := ""

	// Check immediately on start
	checkCode := func() {
		code, err := pm.LoadPairingCode()
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

func (pm *PairingManager) SavePairingCode(code string) error {
	pairingPath := getPairingPath()

	// Create directory if it doesn't exist
	if dir := filepath.Dir(pairingPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return utils.WriteFile(pairingPath, []byte(code))
}

func (pm *PairingManager) LoadPairingCode() (string, error) {
	pairingPath := getPairingPath()
	data, err := utils.ReadFile(pairingPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (pm *PairingManager) DeletePairingCode() error {
	pairingPath := getPairingPath()
	return utils.DeleteFile(pairingPath)
}

// GetBlacklistStatus returns the current blacklist status
func (pm *PairingManager) GetBlacklistStatus() map[string]time.Time {
	pm.blacklistMutex.Lock()
	defer pm.blacklistMutex.Unlock()

	result := make(map[string]time.Time)
	for ip, expiry := range pm.ipBlacklist {
		if time.Now().Before(expiry) {
			result[ip] = expiry
		}
	}
	return result
}

// ClearBlacklist manually clears all blacklist entries (for admin use)
func (pm *PairingManager) ClearBlacklist() {
	pm.blacklistMutex.Lock()
	defer pm.blacklistMutex.Unlock()

	pm.ipBlacklist = make(map[string]time.Time)
	pm.ipViolations = make(map[string]int)
	log.Println("All blacklist entries cleared")
}

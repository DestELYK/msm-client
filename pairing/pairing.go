package pairing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math/rand"
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

	"github.com/golang-jwt/jwt/v5"
	qrcode "github.com/skip2/go-qrcode"
)

var (
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

const DEFAULT_PATH = "/var/lib/msm-client" // Default path for pairing file
const pairingCodeFile = "pairing_code.txt"
const maxFailCount = 3

// IP security constants
const maxIPViolations = 3                 // Max IP violations before blacklisting
const ipBlacklistDuration = 1 * time.Hour // How long to blacklist an IP

func init() {
	// Initialize IP tracking maps
	ipBlacklist = make(map[string]time.Time)
	ipViolations = make(map[string]int)
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
func isIPBlacklisted(ip string) bool {
	blacklistMutex.Lock()
	defer blacklistMutex.Unlock()

	if expiry, exists := ipBlacklist[ip]; exists {
		if time.Now().Before(expiry) {
			return true
		}
		// Clean up expired blacklist entry
		delete(ipBlacklist, ip)
	}
	return false
}

// recordIPViolation records a violation for an IP and blacklists if necessary
func recordIPViolation(ip string) bool {
	blacklistMutex.Lock()
	defer blacklistMutex.Unlock()

	// Increment violation count
	ipViolations[ip]++
	violations := ipViolations[ip]

	log.Printf("IP violation recorded for %s: %d/%d violations", ip, violations, maxIPViolations)

	// Blacklist if max violations reached
	if violations >= maxIPViolations {
		ipBlacklist[ip] = time.Now().Add(ipBlacklistDuration)
		log.Printf("IP %s blacklisted for %v due to %d violations", ip, ipBlacklistDuration, violations)
		return true
	}

	return false
}

// cleanupBlacklist removes expired blacklist entries
func cleanupBlacklist() {
	blacklistMutex.Lock()
	defer blacklistMutex.Unlock()

	now := time.Now()
	for ip, expiry := range ipBlacklist {
		if now.After(expiry) {
			delete(ipBlacklist, ip)
			delete(ipViolations, ip) // Also reset violation count
			log.Printf("Removed expired blacklist entry for IP %s", ip)
		}
	}
}

// getPairingPath returns the path for the pairing code file based on environment variable or default
func getPairingPath() string {
	if path := os.Getenv("MSC_PAIRING_PATH"); path != "" {
		return filepath.Join(path, pairingCodeFile)
	}
	return filepath.Join(DEFAULT_PATH, pairingCodeFile)
}

const pairingCodeCleanupInterval = 5 * time.Second
const pairingCodeExpiry = 1 * time.Minute
const pairingCodeLength = 6

// getTemplatePath returns the path to the pairing display template
func getTemplatePath() string {
	// Check if custom template path is set via environment variable
	if customPath := os.Getenv("MSC_TEMPLATE_PATH"); customPath != "" {
		templatePath := filepath.Join(customPath, "pairing_display.html")
		log.Printf("Using custom template path: %s", templatePath)
		return templatePath
	}

	// Default to templates directory relative to executable
	execDir, err := os.Executable()
	if err == nil {
		templatePath := filepath.Join(filepath.Dir(execDir), "templates", "pairing_display.html")
		if _, err := os.Stat(templatePath); err == nil {
			return templatePath
		}
	}

	// Fallback to current working directory
	templatePath := filepath.Join("templates", "pairing_display.html")
	log.Printf("Using fallback template path: %s", templatePath)
	return templatePath
}

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
	StartPairingServerOnPort(cfg, 49174, false)
}

func StartPairingServerWithDisplay(cfg config.ClientConfig, enableDisplay bool) {
	StartPairingServerOnPort(cfg, 49174, enableDisplay)
}

// StartPairingServerOnPort starts the pairing server on a specific port (for testing)
func StartPairingServerOnPort(cfg config.ClientConfig, port int, enableDisplay bool) {
	// Set global configuration first (even in test mode)
	SetConfig(cfg)

	showingDisplay = enableDisplay

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

	// Add pairing display route if enabled
	if enableDisplay {
		mux.HandleFunc("/display", HandleQRCodeDisplay(cfg))
	}

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
				// Clean up expired pairing codes
				codeMutex.Lock()
				if time.Now().After(expiry) && pairCode != "" {
					log.Printf("Pairing code '%s' expired, invalidating code (had %d failed attempts)", pairCode, failCount)
					pairCode = ""
					pairCodeIP = ""
					expiry = time.Time{}
					failCount = 0
					_ = DeletePairingCode()
				} else if failCount >= maxFailCount && pairCode != "" {
					log.Printf("Max pairing attempts reached for code '%s' (%d/%d failed attempts), invalidating code", pairCode, failCount, maxFailCount)
					pairCode = ""
					pairCodeIP = ""
					expiry = time.Time{}
					failCount = 0
					_ = DeletePairingCode()
				}
				codeMutex.Unlock()

				// Clean up expired blacklist entries
				cleanupBlacklist()
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
		// Get client IP
		clientIP := getClientIP(r)

		// Check if IP is blacklisted
		if isIPBlacklisted(clientIP) {
			log.Printf("Pairing request rejected: IP %s is blacklisted", clientIP)
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		codeMutex.Lock()
		defer codeMutex.Unlock()

		// Check if a valid pairing code already exists
		if pairCode != "" && time.Now().Before(expiry) {
			log.Printf("Pairing code request from IP %s: existing valid code '%s' still active, expires at %s", clientIP, pairCode, expiry.Local().Format(time.RFC3339))

			// Return the existing code information
			message := "Pairing code already active, "
			if showingDisplay {
				message += "please check /display for the code."
			} else {
				message += "please check device for code."
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": message,
				"expiry":  expiry.Format(time.RFC3339),
			})
			return
		}

		// Generate new code only if no valid code exists
		pairCode = GenerateCode()
		pairCodeIP = clientIP
		expiry = time.Now().Add(pairingCodeExpiry)
		failCount = 0

		log.Printf("Generated pairing code: %s for IP %s, expires at %s", pairCode, clientIP, expiry.Local().Format(time.RFC3339))

		SavePairingCode(pairCode)

		// Trigger pairing started callback
		triggerOnPairingStarted(pairCode, expiry)

		message := "Pairing code generated, "

		if showingDisplay {
			message += "please check /display for the code."
		} else {
			message += "please check device for code."
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": message,
			"expiry":  expiry.Format(time.RFC3339),
		})
	}
}

func HandleConfirm(cfg config.ClientConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		clientIP := getClientIP(r)

		// Check if IP is blacklisted
		if isIPBlacklisted(clientIP) {
			log.Printf("Pairing confirmation rejected: IP %s is blacklisted", clientIP)
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		var req struct {
			Code     string `json:"code"`
			ServerWs string `json:"serverWs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Pairing attempt failed: invalid request format from IP %s", clientIP)
			triggerOnPairingFailed("invalid_request", failCount)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		codeMutex.Lock()
		defer codeMutex.Unlock()

		log.Printf("Pairing attempt received from IP %s: code='%s' (attempt %d/%d)", clientIP, req.Code, failCount+1, maxFailCount)

		// Check if IP matches the one that generated the code
		if pairCodeIP != "" && clientIP != pairCodeIP {
			log.Printf("Pairing attempt rejected: IP mismatch. Code generated by %s, attempt from %s", pairCodeIP, clientIP)

			// Record violation and potentially blacklist the IP
			recordIPViolation(clientIP)

			triggerOnPairingFailed("ip_mismatch", failCount)
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

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

		log.Printf("Pairing successful! Code '%s' accepted from IP %s. Connecting to %s", req.Code, clientIP, req.ServerWs)
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

func HandleQRCodeDisplay(cfg config.ClientConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set content type to HTML
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Add cache control headers to ensure fresh content
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		codeMutex.Lock()
		currentCode := pairCode
		currentExpiry := expiry
		codeMutex.Unlock()

		// Template data
		data := struct {
			Code        string
			QRCodeImage string
			Expiry      string
		}{}

		// Check if we have a valid code
		if currentCode != "" && time.Now().Before(currentExpiry) {
			data.Code = currentCode
			data.Expiry = currentExpiry.Local().Format("Jan 2, 2006 3:04:05 PM")

			// Generate QR code containing just the pairing code
			if qrCodeData, err := generateQRCode(currentCode); err == nil {
				data.QRCodeImage = base64.StdEncoding.EncodeToString(qrCodeData)
			}
		} else if currentCode != "" && time.Now().After(currentExpiry) {
			// Code has expired - still show it for the frontend to handle the grace period
			data.Code = currentCode
			data.Expiry = currentExpiry.Local().Format("Jan 2, 2006 3:04:05 PM")

			// Generate QR code for expired state (still show it)
			if qrCodeData, err := generateQRCode(currentCode); err == nil {
				data.QRCodeImage = base64.StdEncoding.EncodeToString(qrCodeData)
			}
		}
		// If no code at all, data remains empty and template shows "no code" state

		// Load and parse template from file
		templatePath := getTemplatePath()
		tmpl, err := template.ParseFiles(templatePath)
		if err != nil {
			log.Printf("Template loading error from %s: %v", templatePath, err)
			// Fallback to a simple error message
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "<html><body><h1>Template Error</h1><p>Could not load pairing display template from: %s</p><p>Error: %v</p></body></html>", templatePath, err)
			return
		}

		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}

func generateQRCode(code string) ([]byte, error) {
	// Generate QR code as PNG containing the pairing code
	// The QR code will contain just the pairing code string (e.g., "ABC123")
	png, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		return nil, err
	}
	return png, nil
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
	pairCodeIP = ""
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
	pairingPath := getPairingPath()

	// Create directory if it doesn't exist
	if dir := filepath.Dir(pairingPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return utils.WriteFile(pairingPath, []byte(code))
}

func LoadPairingCode() (string, error) {
	pairingPath := getPairingPath()
	data, err := utils.ReadFile(pairingPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func DeletePairingCode() error {
	pairingPath := getPairingPath()
	return utils.DeleteFile(pairingPath)
}

// GetBlacklistStatus returns the current blacklist status
func GetBlacklistStatus() map[string]time.Time {
	blacklistMutex.Lock()
	defer blacklistMutex.Unlock()

	result := make(map[string]time.Time)
	for ip, expiry := range ipBlacklist {
		if time.Now().Before(expiry) {
			result[ip] = expiry
		}
	}
	return result
}

// ClearBlacklist manually clears all blacklist entries (for admin use)
func ClearBlacklist() {
	blacklistMutex.Lock()
	defer blacklistMutex.Unlock()

	ipBlacklist = make(map[string]time.Time)
	ipViolations = make(map[string]int)
	log.Println("All blacklist entries cleared")
}

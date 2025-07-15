package pairing

import (
	"context"
	"encoding/json"
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
	pairCode      string
	expiry        time.Time
	failCount     int
	codeMutex     sync.Mutex
	pairServer    *http.Server
	cancelCleanup context.CancelFunc
)

const pairingCodeFile = "pairing_code.txt"
const maxFailCount = 3
const pairingCodeCleanupInterval = 5 * time.Second
const pairingCodeExpiry = 5 * time.Minute
const pairingCodeLength = 6

func StartPairingServer(cfg config.ClientConfig) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pair", HandlePair(cfg))
	mux.HandleFunc("/pair/confirm", HandleConfirm(cfg))

	pairServer = &http.Server{
		Addr:    ":6969",
		Handler: mux,
	}

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

	log.Println("Pairing server started on :6969")
	if err := pairServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Pairing server failed: %v", err)
	}
	log.Println("Pairing server stopped")
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
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		codeMutex.Lock()
		defer codeMutex.Unlock()

		log.Printf("Pairing attempt received: code='%s' (attempt %d/%d)", req.Code, failCount+1, maxFailCount)

		if time.Now().After(expiry) || failCount >= 3 {
			log.Printf("Pairing attempt rejected: code expired or max attempts reached (failCount: %d)", failCount)
			http.Error(w, "Code expired or max attempts", http.StatusForbidden)
			return
		}
		if req.Code != pairCode {
			failCount++
			log.Printf("Pairing attempt failed: incorrect code '%s' (expected '%s'). Fail count: %d/%d", req.Code, pairCode, failCount, maxFailCount)
			http.Error(w, "Incorrect code", http.StatusUnauthorized)
			return
		}

		log.Printf("Pairing successful! Code '%s' accepted. Connecting to %s", req.Code, req.ServerWs)
		token := CreateJWT(cfg.ClientID)
		state.SaveState(state.PairedState{ServerWs: req.ServerWs, Token: token})

		// Cancel the cleanup goroutine since pairing was successful
		if cancelCleanup != nil {
			cancelCleanup()
			cancelCleanup = nil
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "paired",
			"token":   token,
		})

		go func() {
			// Give the HTTP response time to be sent
			time.Sleep(100 * time.Millisecond)
			_ = pairServer.Shutdown(context.Background())
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
}

func StopPairingServer() {
	if pairServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pairServer.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down pairing server: %v", err)
		} else {
			log.Println("Pairing server stopped")
		}
		pairServer = nil
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

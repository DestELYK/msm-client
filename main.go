package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"msm-client/config"
	"msm-client/pairing"
	"msm-client/state"
	"msm-client/ws"

	"github.com/akamensky/argparse"
)

const DEFAULT_PAIRING_PORT = 49174 // Default port for pairing server

// Global variables for graceful shutdown
var (
	shutdownMutex  sync.Mutex
	isShuttingDown bool
	wsm            = ws.NewWebSocketManager()    // WebSocket manager instance
	pm             = pairing.NewPairingManager() // Pairing manager instance
)

// setupSignalHandler sets up graceful shutdown on interrupt signals
func setupSignalHandler() chan os.Signal {
	// Create a channel to receive OS signals
	signalChan := make(chan os.Signal, 1)

	// Register the channel to receive specific signals
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	return signalChan
}

// gracefulShutdown handles the shutdown process
func gracefulShutdown() {
	shutdownMutex.Lock()
	defer shutdownMutex.Unlock()

	if isShuttingDown {
		return
	}
	isShuttingDown = true

	log.Println("Graceful shutdown initiated...")

	// Disconnect WebSocket if connected
	if wsm.IsConnected() {
		log.Println("Disconnecting WebSocket...")
		wsm.ShutdownWebSocket(true)
		time.Sleep(100 * time.Millisecond) // Allow time for disconnect message
	}

	// Stop pairing server
	if pm.IsServerRunning() {
		log.Println("Stopping pairing server...")
		pm.StopPairingServer()
	}

	log.Println("Shutdown complete")
}

func main() {
	parser := argparse.NewParser("msm-client", "MediaScreen Manager Client")

	startCmd := parser.NewCommand("start", "Start the client")
	displayNameFlag := startCmd.String("", "device-name", &argparse.Options{
		Required: false,
		Help:     "Optional friendly name for the device",
	})
	disableCommandsFlag := startCmd.Flag("", "disable-commands", &argparse.Options{
		Required: false,
		Help:     "Disable execution of remote commands (reboot, etc.) for security",
	})
	enableDisplayFlag := startCmd.Flag("", "enable-display", &argparse.Options{
		Required: false,
		Help:     "Enable pairing display server at /display endpoint",
	})
	pairingPortFlag := startCmd.Int("", "pairing-port", &argparse.Options{
		Required: false,
		Default:  DEFAULT_PAIRING_PORT,
		Help:     "Specify the port for the pairing server (default is 49174)",
	})
	ipValidationFlag := startCmd.String("", "ip-validation", &argparse.Options{
		Required: false,
		Default:  "subnet",
		Help:     "Set IP validation mode for pairing: strict, subnet, permissive, or disabled",
	})
	maxIPViolationsFlag := startCmd.Int("", "max-ip-violations", &argparse.Options{
		Required: false,
		Help:     "Maximum IP violations before blacklisting (default: 3)",
	})
	ipBlacklistDurationFlag := startCmd.String("", "ip-blacklist-duration", &argparse.Options{
		Required: false,
		Help:     "Duration to blacklist violating IPs (e.g., '1h', '30m', '2h30m')",
	})
	verificationCodeLengthFlag := startCmd.Int("", "verification-code-length", &argparse.Options{
		Required: false,
		Help:     "Length of verification/pairing code (default: 6)",
	})
	verificationCodeAttemptsFlag := startCmd.Int("", "verification-code-attempts", &argparse.Options{
		Required: false,
		Help:     "Maximum attempts for verification/pairing code (default: 3)",
	})
	pairingCodeExpirationFlag := startCmd.String("", "pairing-code-expiration", &argparse.Options{
		Required: false,
		Help:     "Duration before pairing codes expire (e.g., '1m', '30s', '2m30s')",
	})
	screenSwitchPathFlag := startCmd.String("", "screen-switch-path", &argparse.Options{
		Required: false,
		Help:     "Path to screen switch script (default: /usr/local/bin/mediascreen-installer/scripts/screen-switch.sh)",
	})

	// Pairing command
	pairingCmd := parser.NewCommand("pairing", "Pairing operations")
	getCmd := pairingCmd.NewCommand("get", "Get the current pairing code")
	getWatchFlag := getCmd.Flag("w", "watch", &argparse.Options{
		Required: false,
		Help:     "Watch for pairing code changes",
	})
	resetCmd := pairingCmd.NewCommand("reset", "Reset pairing (delete code and state)")

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
		return
	}

	if startCmd.Happened() {
		fmt.Println("Starting MediaScreen Manager Client...")

		// Set up signal handler for graceful shutdown
		signalChan := setupSignalHandler()

		// Start shutdown handler in a goroutine
		go func() {
			<-signalChan
			log.Println("Received shutdown signal...")
			gracefulShutdown()
			os.Exit(0)
		}()

		cfg, err := config.LoadOrCreateConfig()
		if err != nil {
			log.Fatalf("Invalid config: %v", err)
		}

		// Set device name if provided
		if displayNameFlag != nil && *displayNameFlag != "" {
			cfg.DeviceName = *displayNameFlag
			log.Printf("Device name set to: %s", cfg.DeviceName)
		} else if cfg.DeviceName != "" {
			log.Printf("Device name: %s", cfg.DeviceName)
		} else {
			log.Println("Device name not set")
		}

		// Set command execution flag based on command line argument
		if *disableCommandsFlag {
			cfg.DisableCommands = true
			log.Println("Command execution disabled via command line flag")
		}

		// Set IP validation mode based on command line argument
		if ipValidationFlag != nil && *ipValidationFlag != "" {
			switch *ipValidationFlag {
			case "strict":
				cfg.SetStrictIPValidation()
				log.Println("IP validation mode set to: strict (exact IP match required)")
			case "subnet":
				cfg.SetSubnetIPValidation()
				log.Println("IP validation mode set to: subnet (same subnet allowed)")
			case "permissive":
				cfg.SetPermissiveIPValidation()
				log.Println("IP validation mode set to: permissive (flexible validation)")
			case "disabled":
				cfg.DisableAllIPValidation()
				log.Println("IP validation mode set to: disabled (no IP checking)")
			default:
				log.Printf("Invalid IP validation mode '%s', using current setting: %s", *ipValidationFlag, cfg.GetIPValidationMode())
			}
		} else {
			log.Printf("IP validation mode: %s", cfg.GetIPValidationMode())
		}

		// Set security settings based on command line arguments
		if maxIPViolationsFlag != nil && *maxIPViolationsFlag > 0 {
			cfg.MaxIPViolations = *maxIPViolationsFlag
			log.Printf("Max IP violations set to: %d", cfg.MaxIPViolations)
		}

		if ipBlacklistDurationFlag != nil && *ipBlacklistDurationFlag != "" {
			if duration, err := time.ParseDuration(*ipBlacklistDurationFlag); err == nil && duration >= 0 {
				cfg.IPBlacklistDuration = duration
				log.Printf("IP blacklist duration set to: %v", cfg.IPBlacklistDuration)
			} else {
				log.Printf("Invalid IP blacklist duration '%s', using current setting: %v", *ipBlacklistDurationFlag, cfg.GetIPBlacklistDuration())
			}
		}

		// Set verification code settings based on command line arguments
		if verificationCodeLengthFlag != nil && *verificationCodeLengthFlag > 0 {
			cfg.VerificationCodeLength = *verificationCodeLengthFlag
			log.Printf("Verification code length set to: %d", cfg.VerificationCodeLength)
		}

		if verificationCodeAttemptsFlag != nil && *verificationCodeAttemptsFlag > 0 {
			cfg.VerificationCodeAttempts = *verificationCodeAttemptsFlag
			log.Printf("Verification code attempts set to: %d", cfg.VerificationCodeAttempts)
		}

		if pairingCodeExpirationFlag != nil && *pairingCodeExpirationFlag != "" {
			if duration, err := time.ParseDuration(*pairingCodeExpirationFlag); err == nil && duration > 0 {
				cfg.PairingCodeExpiration = duration
				log.Printf("Pairing code expiration set to: %v", cfg.PairingCodeExpiration)
			} else {
				log.Printf("Invalid pairing code expiration '%s', using current setting: %v", *pairingCodeExpirationFlag, cfg.GetPairingCodeExpiration())
			}
		}

		if screenSwitchPathFlag != nil && *screenSwitchPathFlag != "" {
			cfg.ScreenSwitchPath = *screenSwitchPathFlag
			log.Printf("Screen switch path set to: %s", cfg.ScreenSwitchPath)
		}

		log.Println("MSM Client started. Press Ctrl+C to exit gracefully.")

		savedState, err := state.LoadState()
		if err == nil {
			log.Printf("Found saved state, connecting to %s", savedState.ServerWs)
			for {
				// Check if we're shutting down
				shutdownMutex.Lock()
				if isShuttingDown {
					shutdownMutex.Unlock()
					break
				}
				shutdownMutex.Unlock()

				wsm.ConnectWebSocket(cfg, savedState.ServerWs)

				shutdownMutex.Lock()
				if isShuttingDown {
					shutdownMutex.Unlock()
					break
				}
				shutdownMutex.Unlock()

				// If ConnectWebSocket returns, it means the connection was lost
				// and the state file was deleted (triggering restart)
				log.Println("WebSocket connection ended, checking if pairing is needed...")

				// Check if state still exists
				if state.HasState() {
					log.Println("State still exists, attempting to reconnect...")
					// Reload state in case it changed
					if newState, loadErr := state.LoadState(); loadErr == nil {
						savedState = newState
						continue
					} else {
						log.Printf("Error reloading state: %v", loadErr)
						break
					}
				} else {
					log.Println("State file deleted, starting pairing server...")
					break // Exit loop to start pairing
				}
			}
		}

		// Start pairing server (either because no saved state initially, or state was deleted)
		for {
			// Check if we're shutting down
			shutdownMutex.Lock()
			if isShuttingDown {
				shutdownMutex.Unlock()
				break
			}
			shutdownMutex.Unlock()

			if *enableDisplayFlag {
				log.Println("Pairing display enabled - web interface available at /display")
			}
			pm.StartPairingServerOnPort(cfg, *pairingPortFlag, *enableDisplayFlag)

			// After pairing server stops, check if we now have saved state
			// This happens when pairing was successful
			pairedState, stateErr := state.LoadState()
			if stateErr == nil {
				log.Printf("Pairing completed! Connecting to %s", pairedState.ServerWs)
				wsm.ConnectWebSocket(cfg, pairedState.ServerWs)
				// If we get here, the WebSocket connection ended and might need to restart pairing
				continue
			} else {
				log.Println("Pairing server stopped without successful pairing")
				break
			}
		}
	}

	// Handle pairing command
	if pairingCmd.Happened() {
		if getCmd.Happened() {
			if *getWatchFlag {
				fmt.Println("Watching for pairing code changes...")
				pm.WatchPairingCode(5 * time.Second)
				return
			}

			code, expiry := pm.GetPairingCode()
			if code == "" {
				fmt.Println("No pairing code available or it has expired.")
				return
			}
			fmt.Printf("Pairing code: %s (expires at %s)\n", code, expiry.Format(time.RFC3339))
			return
		}

		if resetCmd.Happened() {
			if !state.HasState() {
				fmt.Println("Not paired yet. Nothing to reset.")
				return
			}

			pairingCodeErr := pm.DeletePairingCode()
			if pairingCodeErr != nil {
				log.Fatalf("Failed to reset pairing: %v", pairingCodeErr)
			}

			stateErr := state.DeleteState()
			if stateErr != nil {
				log.Fatalf("Failed to delete state: %v", stateErr)
			}

			fmt.Println("Pairing reset successfully.")
			return
		}
	}
}

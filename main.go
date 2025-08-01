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
		Help:     "Specify the port for the pairing server (default is 47174)",
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

		if err := config.LoadEnv(); err != nil {
			log.Fatalf("Error loading environment variables: %v", err)
		}

		cfg, err := config.LoadOrCreateConfig()
		if err != nil {
			log.Fatalf("Invalid config: %v", err)
		}

		// Set command execution flag based on command line argument
		if *disableCommandsFlag {
			cfg.DisableCommands = true
			log.Println("Command execution disabled via command line flag")
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

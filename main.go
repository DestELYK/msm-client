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

// Global variables for graceful shutdown
var (
	shutdownMutex  sync.Mutex
	isShuttingDown bool
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
	if ws.IsConnected() {
		log.Println("Disconnecting WebSocket...")
		ws.ShutdownWebSocket()
		time.Sleep(100 * time.Millisecond) // Allow time for disconnect message
	}

	// Stop pairing server
	if pairing.IsServerRunning() {
		log.Println("Stopping pairing server...")
		pairing.StopPairingServer()
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

				ws.ConnectWebSocket(cfg, savedState.ServerWs, savedState.Token)

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

			pairing.StartPairingServer(cfg)

			// After pairing server stops, check if we now have saved state
			// This happens when pairing was successful
			pairedState, stateErr := state.LoadState()
			if stateErr == nil {
				log.Printf("Pairing completed! Connecting to %s", pairedState.ServerWs)
				ws.ConnectWebSocket(cfg, pairedState.ServerWs, pairedState.Token)
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
				pairing.WatchPairingCode(5 * time.Second)
				return
			}

			code, expiry := pairing.GetPairingCode()
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

			pairingCodeErr := pairing.DeletePairingCode()
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

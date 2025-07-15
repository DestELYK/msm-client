package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"msm-client/config"
	"msm-client/pairing"
	"msm-client/state"
	"msm-client/ws"

	"github.com/akamensky/argparse"
)

func main() {
	parser := argparse.NewParser("msm-client", "MediaScreen Manager Client")

	startCmd := parser.NewCommand("start", "Start the client")

	// Pairing command
	pairingCmd := parser.NewCommand("pairing", "Pairing operations")
	watchFlag := pairingCmd.Flag("w", "watch", &argparse.Options{
		Required: false,
		Help:     "Watch for pairing code changes",
	})
	deleteFlag := pairingCmd.Flag("d", "delete", &argparse.Options{
		Required: false,
		Help:     "Delete the current pairing code",
	})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
		return
	}

	if startCmd.Happened() {
		fmt.Println("Starting MediaScreen Manager Client...")

		if err := config.LoadEnv(); err != nil {
			log.Fatalf("Error loading environment variables: %v", err)
		}

		cfg, err := config.LoadOrCreateConfig()
		if err != nil {
			log.Fatalf("Invalid config: %v", err)
		}

		savedState, err := state.LoadState()
		if err == nil {
			log.Printf("Found saved state, connecting to %s", savedState.ServerWs)
			ws.ConnectWebSocket(cfg, savedState.ServerWs, savedState.Token)
			return
		}

		pairing.StartPairingServer(cfg)

		// After pairing server stops, check if we now have saved state
		// This happens when pairing was successful
		pairedState, stateErr := state.LoadState()
		if stateErr == nil {
			log.Printf("Pairing completed! Connecting to %s", pairedState.ServerWs)
			ws.ConnectWebSocket(cfg, pairedState.ServerWs, pairedState.Token)
		} else {
			log.Println("Pairing server stopped without successful pairing")
		}
	}

	// Handle pairing command
	if pairingCmd.Happened() {
		if *watchFlag {
			fmt.Println("Watching for pairing code changes...")
			pairing.WatchPairingCode(5 * time.Second)
			return
		}

		if *deleteFlag {
			err := pairing.DeletePairingCode()
			if err != nil {
				log.Fatalf("Failed to delete pairing code: %v", err)
			}
			fmt.Println("Pairing code deleted.")
			return
		}

		// Default: show pairing code
		code, err := pairing.LoadPairingCode()
		if err != nil {
			log.Fatalf("Failed to get pairing code: %v", err)
		}
		fmt.Printf("Pairing code: %s\n", code)
		return
	}
}

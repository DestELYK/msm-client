package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type PairedState struct {
	ServerWs string `json:"server_ws"`
	Token    string `json:"token"`
}

const DEFAULT_PATH = "/var/lib/msm-client" // Default path for state file
const stateFile = "paired.json"

// getStatePath returns the path for the state file based on environment variable or default
func getStatePath() string {
	if path := os.Getenv("MSC_STATE_PATH"); path != "" {
		return filepath.Join(path, stateFile)
	}
	return filepath.Join(DEFAULT_PATH, stateFile)
}

func SaveState(state PairedState) error {
	statePath := getStatePath()

	// Create directory if it doesn't exist
	if dir := filepath.Dir(statePath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, data, 0600)
}

func LoadState() (PairedState, error) {
	var state PairedState
	statePath := getStatePath()
	data, err := os.ReadFile(statePath)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func HasState() bool {
	statePath := getStatePath()
	_, err := os.Stat(statePath)
	return !os.IsNotExist(err)
}

func DeleteState() error {
	statePath := getStatePath()
	return os.Remove(statePath)
}

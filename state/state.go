package state

import (
	"encoding/json"
	"os"
)

type PairedState struct {
	ServerWs string `json:"server_ws"`
	Token    string `json:"token"`
}

const stateFile = "paired.json"

func SaveState(state PairedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile, data, 0600)
}

func LoadState() (PairedState, error) {
	var state PairedState
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

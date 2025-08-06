package fileutils

import (
	"encoding/json"
	"fmt"
	defs "mica-shim/definitions"
	"os"
	"path/filepath"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func LoadStateFromDir(bundle string) (*specs.State, error) {
	var state specs.State
	statePath := filepath.Join(bundle, defs.MicantainerStateFile)
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read container state from %s: %w", statePath, err)
	}
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal container state from %s: %w", statePath, err)
	}
	return &state, nil
}

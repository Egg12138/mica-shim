package fileutils

import (
	defs "mica-shim/definitions"
	"os"
	"path/filepath"

	log "mica-shim/logger"
)

// remove state file in micran state directory
func RemoveExternalStatFile(id string) error {
	// if the file does not exist, return nil
	path := filepath.Join(defs.MicranStateDir, id, defs.MicantainerStateFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	log.Debugf("removing state file: %s", path)
	return os.Remove(path)
}

func RemoveStateDir(id string) error {
	return os.RemoveAll(filepath.Join(defs.MicranStateDir, id))
}
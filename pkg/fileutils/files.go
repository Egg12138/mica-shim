package fileutils

import (
	"fmt"
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

func IsSymlink(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeSymlink != 0
}

// ResolvePath returns the fully resolved and expanded value of the
// specified path.
func ResolvePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path must be specified")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file %v does not exist", absolute)
		}

		return "", err
	}

	return resolved, nil
}

func SetReadonly(path string) error {
	// assume path is a valid direntry
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0444)
		if info.IsDir() {
			mode = os.FileMode(0555)
		}
		return os.Chmod(path, mode)
	})
}

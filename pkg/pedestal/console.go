package pedestal

import (
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/utils"
	"os"
)

// ConsolePTYPathForDomain resolves the PTY path published by xl console for a given domain.
func ConsolePTYPathForDomain(id string) (string, error) {
	shortID := utils.ShortID(id)

	ptyPath, err := xenStoreRead(shortID, "console/tty")
	if err != nil {
		return "", fmt.Errorf("failed to read PTY path from XenStore: %w", err)
	}

	log.Debugf("PTY path for domain %s: %s", shortID, ptyPath)
	path := ptyPath
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("console PTY %s not found: %w", path, err)
	}

	return path, nil
}

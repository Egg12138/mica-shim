package utils

import (
	"errors"
	"fmt"
	log "mica-shim/logger"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const MAX_ID_LENGTH = 31
const validCIDRegex = "^[a-zA-Z0-9][a-zA-Z0-9_.-]+$"

func ValidContainerID(id string) error {
	if id == "" {
		return fmt.Errorf("container ID cannot be empty")
	}

	if len(id) > MAX_ID_LENGTH {
		log.Debugf("container is %s too long, we will pass a new shorted ID %s to micad", id, ShortID(id))
	}

	pattern := regexp.MustCompile(validCIDRegex)
	matched := pattern.MatchString(id)
	if !matched {
		return fmt.Errorf("invalid container/sandbox ID: %s", id)
	}
	return nil
}

// Truncate the ID to the maximum allowed length.
// Truncating the original hash is good at collision resistance.
func truncateID(id string) string {
	idBytes := []byte(id)
	if len(idBytes) > MAX_ID_LENGTH {
		idBytes = idBytes[:MAX_ID_LENGTH]
	}
	return string(idBytes)
}

func ShortID(id string) string {
	return truncateID(id)
}

func IdMatched(longID string, shortID string) bool {
	return truncateID(longID) == shortID
}

func FileExist(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// EnsureDir check if a directory exist, if not then create it
func EnsureDir(path string, mode os.FileMode) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("not an absolute path: %s", path)
	}

	if fi, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(path, mode); err != nil {
				return err
			}
		} else {
			return err
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}

	return nil
}

func InList(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

// LsofSocket returns a slice of PIDs using the given socket path
// It runs lsof to check which processes are using the socket
func LsofSocket(socketPath string, command string) []int {
	var pids []int
	cmd := exec.Command("lsof", socketPath)
	output, err := cmd.Output()
	if err != nil {
		log.Debugf("Failed to run lsof on %s: %v", socketPath, err)
		return pids
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "COMMAND") || line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if fields[0] != command {
				continue
			}
			if pid, err := strconv.Atoi(fields[1]); err == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

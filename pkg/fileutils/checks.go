package fileutils

import (
	"fmt"
	log "mica-shim/logger"
	"regexp"
)

const MAX_ID_LENGTH = 32
const validCIDRegex = `^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`

// TODO: move `error` into string: we will return a newid with proper length
func ValidContainerID(id string) error {
	if id == "" {
		return fmt.Errorf("empty is is not allowed")
	}

	if len(id) > MAX_ID_LENGTH {
		log.Debugf("container is %s too long, we will return a new shorted ID[future]", id)
	}

	pattern := regexp.MustCompile(validCIDRegex)
	matched := pattern.MatchString(id)
	if !matched {
		return fmt.Errorf("invalid container/sandbox id: %s", id)
	}
	return nil
}

// Truncated the original hash is good at collision resistance
func truncateID(id string) string {
	idBytes := []byte(id)
	if len(idBytes) > MAX_ID_LENGTH {
		idBytes = idBytes[:MAX_ID_LENGTH]
	}
	return string(idBytes)
}

func IdMatched(longID string, shortID string) bool {
	return truncateID(longID) == shortID
}

func ShortID(id string) string {
	return truncateID(id)
}

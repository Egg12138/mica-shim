package fileutils

import (
	"bufio"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"os"
	"strings"
)

// stripQuotes removes surrounding quotes from a string if both start and end quotes match
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// a faster ini parsing method, by reading line by line
// Add checks for new syntax: "1,3-5"
func ParseConfigINI(configPath string) (map[string]string, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// If config file doesn't exist, return empty map (not an error)
		return make(map[string]string), nil
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mica config file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inMicaSection := false

	// Pre-allocate map for faster lookups
	parsedFields := make(map[string]string, 8)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if len(line) == 0 || line[0] == '#' || line[0] == ';' {
			continue
		}

		if line[0] == '[' && line[len(line)-1] == ']' {
			sectionName := strings.ToLower(line[1 : len(line)-1])
			inMicaSection = InList(defs.OKSectionList[:], sectionName)
			continue
		}

		if !inMicaSection {
			continue
		}

		// Find the separator (= or :)
		sepIndex := strings.IndexByte(line, '=')
		if sepIndex == -1 {
			sepIndex = strings.IndexByte(line, ':')
		}
		if sepIndex == -1 {
			continue // Skip malformed lines
		}

		key := strings.ToLower(strings.TrimSpace(line[:sepIndex]))
		value := strings.TrimSpace(line[sepIndex+1:])

		// Remove surrounding quotes if present
		value = stripQuotes(value)

		parsedFields[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading mica config file: %v", err)
	}

	log.Pretty("parsed ini conf: %v", parsedFields)

	return parsedFields, nil
}

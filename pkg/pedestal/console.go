package pedestal

import (
	"bufio"
	"bytes"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/utils"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ConsolePTYPathForDomain resolves the PTY path published by xl console for a given domain.
func ConsolePTYPathForDomain(id string) (string, error) {
	if defs.IsMock {
		return "", fmt.Errorf("console PTY fallback is unavailable in mock mode")
	}

	shortID := utils.ShortID(id)

	domid, err := domainID(shortID)
	if err != nil {
		return "", err
	}

	path := fmt.Sprintf("/dev/pts/%d", domid+1)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("console PTY %s not found: %w", path, err)
	}

	return path, nil
}

func domainID(shortID string) (int, error) {
	if domid, err := runXLDomid(shortID); err == nil {
		return domid, nil
	} else {
		log.Debugf("xl domid fallback failed for %s: %v", shortID, err)
	}

	return parseXLListForDomain(shortID)
}

func runXLDomid(shortID string) (int, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("xl", "domid", shortID)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return 0, fmt.Errorf("xl domid %s: %s", shortID, msg)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return 0, fmt.Errorf("xl domid %s returned empty output", shortID)
	}

	domid, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("xl domid %s invalid output %q: %w", shortID, out, err)
	}

	return domid, nil
}

func parseXLListForDomain(shortID string) (int, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("xl", "list")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return 0, fmt.Errorf("xl list failed: %s", msg)
	}

	scanner := bufio.NewScanner(strings.NewReader(stdout.String()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Name") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		if fields[0] != shortID {
			continue
		}

		domid, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, fmt.Errorf("xl list returned invalid domid %q for %s: %w", fields[1], shortID, err)
		}
		return domid, nil
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("xl list parse error: %w", err)
	}

	return 0, fmt.Errorf("domain %s not found in xl list output", shortID)
}

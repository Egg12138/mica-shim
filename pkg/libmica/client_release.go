//go:build !debug
// +build !debug

package libmica

import (
	"fmt"
)

// handleMicaUpdateWithXl handles MUpdate commands using xl commands instead of micad set command
// This is a no-op implementation for release builds
func handleMicaUpdateWithXl(id string, opts ...string) error {
	return fmt.Errorf("xl workaround not available in release build")
}
//go:build !oldcri
// +build !oldcri

package shim

// getConfigPathFromOldCRI is a no-op unless built with the 'oldcri' build tag.
// This avoids pulling heavy legacy containerd CRI deps by default.
func getConfigPathFromOldCRI(v any) (string, bool) {
	return "", false
}

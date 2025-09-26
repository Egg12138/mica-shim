//go:build oldcri
// +build oldcri

package shim

import oldcrioption "github.com/containerd/cri-containerd/pkg/api/runtimeoptions/v1"

// getConfigPathFromOldCRI provides backward compatibility for older containerd CRI options.
// This file is included only when built with -tags oldcri to avoid pulling heavy deps by default.
func getConfigPathFromOldCRI(v any) (string, bool) {
	if opt, ok := v.(*oldcrioption.Options); ok {
		return opt.ConfigPath, true
	}
	return "", false
}
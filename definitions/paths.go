//go:build !debug
// +build !debug

package defs

const (
	MicaConfDir        = "/etc/mica"
	MicaStateDir       = "/run/mica"
	MicranStateDir     = "/run/micran"
	MicaContainersRoot = "/run/micran/containers"

	MicantainerStateFile = "state.json"
)

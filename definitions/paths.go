//go:build !debug
// +build !debug

package defs

import "os"

const (
	MicaConfDir        = "/etc/mica"
	MicaStateDir       = "/run/mica"
	DaemonRoot         = "/run"

  DirMode = os.FileMode(0700) | os.ModeDir
  FileMode = os.FileMode(0644)
)

const (
	MicranStateDir     = "/run/micran"
	MicaContainersRoot = "/run/micran/containers"
	MicantainerStateFile = "state.json"
	SandboxStateFile = "state.json"
	// directory for sandbox data storage
	SandboxDataDir          = "/run/micran/sandbox"

)
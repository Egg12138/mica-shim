//go:build debug
// +build debug

// Why we need a target: debug?
// 'cause we need a mock version for rootless tests
// TODO: add a rootless package for easiler conditional functions running

package defs

import "os"

const (
	MicaConfDir  = "/tmp/mica/conf"
	MicaStateDir = "/tmp/mica"
	DaemonRoot   = "/run"

	DirMode  = os.FileMode(0755) | os.ModeDir
	FileMode = os.FileMode(0666)
)

const (
	MicaContainersRoot   = "/tmp/micran/containers"
	MicantainerStateFile = "state.json"
	SandboxStateFile     = "state.json"
	MicranStateDir       = "/tmp/micran"
	SandboxDir           = "/tmp/micran/sandbox"
)

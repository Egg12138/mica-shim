//go:build !debug
// +build !debug

package defs

const (
	ShimSocketPath = "/tmp/mica-shim.sock"
	MicaConfDir    = "/etc/mica"
	MicaSocketDir  = "/run/mica"
)

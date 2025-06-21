//go:build debug
// +build debug

package defs

import "time"

const (
	// ShimName             = "org.openeuler.micashim.v2"
	RuntimeName          = "mica"
	MicaAnnotationPrefix = "org.openeuler.mica"
	MicaSuccess          = "MICA-SUCCESS"
	MicaFailed           = "MICA-FAILED"
	MicaSocketName       = "mica-create.socket"
	MicaCreatSocketPath  = MicaSocketDir + "/" + MicaSocketName
	MicaSocketBufSize    = 512
	MicaSocketTimout     = 5 * time.Second
)

var ShimName string

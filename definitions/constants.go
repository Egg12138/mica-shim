package defs

import "time"

const (
  ShimName = "io.containerd.mica.v2"	
	ShimSocketPath = "/tmp/mica-shim.sock"
	RuntimeName = "mica"
	MicaAnnotationPrefix = "org.openeuler.mica"
	MicaSuccess = "MICA-SUCCESS"
	MicaFailed  = "MICA-FAILED"
	MicaConfDir = "/etc/mica"
	MicaSocketDir = "/run/mica"
	MicaSocketName = "mica-create.socket"
	MicaSocketPath = MicaSocketDir + "/" + MicaSocketName
	MicaSocketBufSize = 512
	MicaSocketTimout = 5 * time.Second
)
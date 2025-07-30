package defs

import "time"

const (
	RuntimeName         = "mica"
	MicaSuccess         = "MICA-SUCCESS"
	MicaFailed          = "MICA-FAILED"
	MicaSocketName      = "mica-create.socket"
	MicaCreatSocketPath = MicaSocketDir + "/" + MicaSocketName
	MicaSocketBufSize   = 512
	MicaSocketTimout    = 5 * time.Second

	SHM_NAME = "/dev/shm/mica_free_cores"
)

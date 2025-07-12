package defs

import "time"

const (
	RuntimeName = "mica"
	// MicaAnnotationPrefix = "org.openeuler.mica"
	MicaAnnotationPrefix = "io.mica"
	SuffixOS             = ".client.os"
	SuffixFirmware       = ".client.firmware"
	SuffixPedestal       = ".client.pedestal"
	SuffixPedestalConf   = ".client.pedestal_conf"
	SuffixNcpu           = ".client.ncpu"
	// Compt+<component>
	Compat = ".client.compatibility"

	MicaSuccess         = "MICA-SUCCESS"
	MicaFailed          = "MICA-FAILED"
	MicaSocketName      = "mica-create.socket"
	MicaCreatSocketPath = MicaSocketDir + "/" + MicaSocketName
	MicaSocketBufSize   = 512
	MicaSocketTimout    = 5 * time.Second

	SHM_NAME = "/dev/shm/mica_free_cores"
)

var (
	PreservedOS = []string{"zephyr", "uniproton", "linux"}
)

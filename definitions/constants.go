package defs

import "time"

const (
	RuntimeName = "mica"
	// MicaLabelPrefix = "org.openeuler.mica"
	MicaAnnotationPrefix = "org.openeuler.mica" // used for runtime-level configurtaion
	MicaLabelPrefix      = "io.mica"

	DefaultClientConf = "client.conf"
	// ini config keys in [Mica] section of client.conf
	OS           = "os"
	Firmware     = "firmware"
	Pedestal     = "pedestal"
	PedestalConf = "pedestal_conf"
	Ncpu         = "ncpu"
	Compat       = "compatibility"

	// these items in client.conf will be ignored: CPU, Name, AutoBoot, Debug,
	// runtime will configure these items by container logic
	// AutoBoot       = "autoboot"
	// Debug          = "debug"
	// CPU            = "cpu"
	// Name           = "name"

	MicaSuccess         = "MICA-SUCCESS"
	MicaFailed          = "MICA-FAILED"
	MicaSocketName      = "mica-create.socket"
	MicaCreatSocketPath = MicaSocketDir + "/" + MicaSocketName
	MicaSocketBufSize   = 512
	MicaSocketTimout    = 5 * time.Second

	SHM_NAME = "/dev/shm/mica_free_cores"
)

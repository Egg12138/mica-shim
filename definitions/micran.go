package defs

import "time"

const (
	RuntimeName         = "mica"
	MicaSuccess         = "MICA-SUCCESS"
	MicaFailed          = "MICA-FAILED"
	MicaSocketName      = "mica-create.socket"
	MicaCreatSocketPath = MicaStateDir + "/" + MicaSocketName
	MicaSocketBufSize   = 512
	MicaSocketTimout    = 5 * time.Second

	SHM_NAME = "/dev/shm/mica_free_cores"
	IsMock   = true
	HostContainerSupports = false
)

// configurations keys
const (
	ConfigStaticResource = "static_resource"
	ConfigKeyClientLimit = "max_client_number"
	ConfigKeyLinuxContainer = "enable_host_container"
	ConfigKeyDebug = "debug"
	ConfigKeyStateDir = "state_dir"
	ConfigKeyPauseImg = "pause_image"
	ConfigKeyMaxContainerVCPU = "max_container_vcpu"
)



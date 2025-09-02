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
)

// OCI and runtime annotations.
const (
	MicraAnnotationPrefix = "io.openeuler.micran." // For runtime-level configuration
	PedConfPrefix         = MicraAnnotationPrefix + "ped."
	RuntimePrefix         = MicraAnnotationPrefix + "runtime."
	ContainerPrefix       = MicraAnnotationPrefix + "container."

	// BundlePathKey is the annotation key for the OCI configuration file path.
	BundlePathKey = MicraAnnotationPrefix + "pkg.oci.bundle_path"
	// ContainerTypeKey is the annotation key for the container type.
	ContainerTypeKey     = MicraAnnotationPrefix + "pkg.oci.container_type"
	SandboxConfigPathKey = MicraAnnotationPrefix + "config_path"
)

// Pedestal configurations.
const (
// Basically about Xen
)

// Configuration for mica clients, passed to the sandbox container.
// NOTICE: micad is shared for all micrans, which means that micad can not be configured differently.
// Hence the freedom degree is limited.
// TODO: an idea, support dynamic configuration loader module for micad.
const (

	// ini config keys in [Mica] section of client.conf
	OSAnnotation = ContainerPrefix + "os"
	FirmwarePath = PedConfPrefix + "firmware_path"
	Pedtype      = PedConfPrefix + "pedestal"
	Compat       = PedConfPrefix + "compatibility"
	// sha-256
	FirmwareHash = PedConfPrefix + "firmware_hash"

	NetPlaceholder = PedConfPrefix + "net_placeholder"
	// DefaultMaxCPU specifies the maximum number of CPUs allocated for the client.
	DefaultMaxCPU = PedConfPrefix + "defautl_max_cpu"
	// DefaulaMemory specifies the maximum byte size of memory assigned for the client.
	DefaulaMemory = PedConfPrefix + "default_memory"

	// Virtio into micran is in out roadmap.
	VirtioMem = PedConfPrefix + "enable_virtio_mem"
	// TODO: more xen-related options
	// PedestalConf = ClientConfPrefix + "pedestalconf"
)

const (
	DisableNewNetNs = RuntimePrefix + "disable_new_netns"
	Experiemental   = RuntimePrefix + "experimental"
	PipeSize        = RuntimePrefix + "pipe_size"
	RuntimeDebug    = RuntimePrefix + "debug"
)

const (
	PauseImage     = "k8s.gcr.io/pause"
	SandboxVersion = 1
)

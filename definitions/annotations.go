package defs

// OCI and runtime annotations.
const (
	// MicraAnnotationPrefix is the prefix for all micran-specific annotations.
	MicraAnnotationPrefix = "io.openeuler.micran." // For runtime-level configuration.
	// PedConfPrefix is the prefix for pedestal-related configurations.
	PedConfPrefix = MicraAnnotationPrefix + "ped."
	// RuntimePrefix is the prefix for runtime-related configurations.
	RuntimePrefix = MicraAnnotationPrefix + "runtime."
	// ContainerPrefix is the prefix for container-related configurations.
	ContainerPrefix = MicraAnnotationPrefix + "container."

	// BundlePathKey is the annotation key for the OCI configuration file path.
	BundlePathKey = MicraAnnotationPrefix + "pkg.oci.bundle_path"
	// ContainerTypeKey is the annotation key for the container type.
	ContainerTypeKey = MicraAnnotationPrefix + "pkg.oci.container_type"
	// SandboxConfigPathKey is the annotation key for the sandbox configuration path.
	SandboxConfigPathKey = MicraAnnotationPrefix + "config_path"
)

// Pedestal configurations.
const (
	// Basically about Xen.
)

// Configuration for mica clients, passed to the sandbox container.
// NOTICE: Micad is shared for all micrans, which means that micad can not be configured differently.
// Hence the freedom degree is limited.
// TODO: An idea, support dynamic configuration loader module for micad.
const (

	// OSAnnotation specifies the client OS type. Corresponds to ini config keys in [Mica] section of client.conf.
	OSAnnotation = ContainerPrefix + "os"
	// FirmwarePath specifies the path to the firmware.
	FirmwarePath = PedConfPrefix + "firmware_path"
	// Pedtype specifies the pedestal type.
	Pedtype = PedConfPrefix + "pedestal"
	// Compat specifies compatibility options.
	Compat = PedConfPrefix + "compatibility"
	// FirmwareHash is the sha-256 hash of the firmware.
	FirmwareHash = PedConfPrefix + "firmware_hash"

	// NetPlaceholder is a placeholder for network configuration.
	NetPlaceholder = PedConfPrefix + "net_placeholder"
	// DefaultMaxCPU specifies the maximum number of CPUs visible in the client.
	// For Xen, ACRN: maxcpus;
	// For openAMP: useless, no vcpu.
	DefaultMaxCPU = PedConfPrefix + "defautl_max_cpu"
	// DefaulaMemory specifies the maximum byte size of memory assigned for the client.
	DefaulaMemory = PedConfPrefix + "default_memory"

	// VirtioMem indicates if virtio-mem is enabled. Virtio support is on our roadmap.
	VirtioMem = PedConfPrefix + "enable_virtio_mem"
	// TODO: Add more xen-related options.
	// PedestalConf = ClientConfPrefix + "pedestalconf"
)

const (
	// DisableNewNetNs disables the creation of a new network namespace.
	DisableNewNetNs = RuntimePrefix + "disable_new_netns"
	// Experiemental enables experimental features.
	Experiemental = RuntimePrefix + "experimental"
	// PipeSize specifies the pipe size for IO.
	PipeSize = RuntimePrefix + "pipe_size"
	// RuntimeDebug enables debug mode for the runtime.
	RuntimeDebug = RuntimePrefix + "debug"
)

const (
	// TODO: We need a special Pause image.
	// PauseImage is the image used for pausing a container.
	PauseImage = "k8s.gcr.io/pause"
	// SandboxVersion is the version of the sandbox.
	SandboxVersion = 1
)
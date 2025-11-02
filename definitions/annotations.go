package defs

// OCI and runtime annotations.
const (
	// MicranAnnotationPrefix is the prefix for all micran-specific annotations.
	MicranAnnotationPrefix = "org.openeuler.micran." // For runtime-level configuration.
	// PedPrefix is the prefix for pedestal-related configurations.
	PedPrefix = MicranAnnotationPrefix + "ped."
	// RuntimePrefix is the prefix for runtime-related configurations.
	RuntimePrefix = MicranAnnotationPrefix + "runtime."
	// ContainerPrefix is the prefix for container-related configurations.
	ContainerPrefix = MicranAnnotationPrefix + "container."
	// CompatPrefix is the prefix for compatibility-related configurations.
	CompatPrefix = MicranAnnotationPrefix + "compatibility."

	// BundlePathKey is the annotation key for the OCI configuration file path.
	BundlePathKey = MicranAnnotationPrefix + "pkg.oci.bundle_path"
	// ContainerTypeKey is the annotation key for the container type.
	ContainerTypeKey = MicranAnnotationPrefix + "pkg.oci.container_type"
	// SandboxConfigPathKey is the annotation key for the sandbox configuration path.
	SandboxConfigPathKey = MicranAnnotationPrefix + "config_path"
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
	// FirmwarePath specifies the relative path to the firmware, in the bundle.
	FirmwarePath = ContainerPrefix + "firmware_path"
	// FirmwareHash is the sha-256 hash of the firmware.
	FirmwareHash = ContainerPrefix + "firmware_hash"
	// Pedtype specifies the pedestal type.
	Pedtype = PedPrefix + "pedestal"
	// PedCompat specifies compatibility options: format "^versionX" (deprecated, use CompatPrefix directly)
	PedCompat = PedPrefix + "compatibility" // DEPRECATED: Use CompatPrefix instead
	// NetPlaceholder is a placeholder for network configuration.
	NetPlaceholder = PedPrefix + "net_placeholder"
	// DefaultMaxCPU specifies the maximum number of CPUs visible in the client.
	// For Xen, ACRN: maxcpus;
	// For openAMP: useless, no vcpu.
	DefaultMaxCPU = PedPrefix + "defautl_max_cpu"
	// DefaulaMemory specifies the maximum byte size of memory assigned for the client.
	DefaulaMemory = PedPrefix + "default_memory"
	PedestalConf  = PedPrefix + "conf"
)

// Container-specific runtime settings.
const (
	// ContainerMinMemMB specifies the initial memory (MiB) assigned to the client at boot.
	// This differs from the max memory limit (Memory/MaxMemMB) that may come from OCI.
	ContainerMinMemMB = ContainerPrefix + "min_memory_mb"
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

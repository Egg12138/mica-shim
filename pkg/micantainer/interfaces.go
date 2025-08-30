package micantainer

// Inspired by kata-containers
import (
	"context"
	"io"
	"mica-shim/pkg/libmica"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// deprecated: useless
type MicantainerManager interface {
	CreateSandbox(ctx context.Context, config SandboxConfig, hookFunc func(context.Context) error) (SandboxTraits, error)
	CleanupContainer(ctx context.Context, sandboxID string, containerID string, force bool) error
}

type ContainerTraits interface {
	// RTOS may contains no the concept of PID, so we use a dummy value
	ID() string
	GetAnnotations() map[string]string
	GetPid() int
	Sandbox() SandboxTraits
	TaskInfo() RTOSTask
	GetMemoryLimit() uint64
	Status() StateString
	State() *ContainerState
	GetClientCPU() (int, error)
	SaveState() error
	Signal(ctx context.Context, signal syscall.Signal) error
}

// some of which required by containerd
type SandboxTraits interface {
	// Identification and state methods
	SandboxID() string
	Annotation(key string) (string, error)
	SetAnnotations(annotations map[string]string)
	AllAnnotations() map[string]string
	DaemonState() *libmica.MicaDaemonState
	Status() SandboxStatus
	GetAllContainers() []ContainerTraits
	GetContainer(id string) ContainerTraits
	GetNetNamespace() string
	Stats(ctx context.Context) (SandboxStats, error)

	// Sandbox Lifecycle methods
	Start(ctx context.Context) error
	Stop(ctx context.Context, force bool) error
	Delete(ctx context.Context) error

	// Container management methods
	CreateContainer(ctx context.Context, config ContainerConfig) (ContainerTraits, error)
	DeleteContainer(ctx context.Context, id string) (ContainerTraits, error)
	StartContainer(ctx context.Context, id string) (ContainerTraits, error)
	StopContainer(ctx context.Context, id string, force bool) (ContainerTraits, error)
	KillContainer(ctx context.Context, id string) (ContainerTraits, error)
	StatusContainer(id string) (ContainerStatus, error)
	StatsContainer(ctx context.Context, id string) (ContainerStats, error)
	IOStream(containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error)
	GetOOMEvent(ctx context.Context) (string, error)
	// Not supported well
	// TODO: aftet unified micran and micad, we can achive sending signals to RTOS clients
	PauseContainer(ctx context.Context, id string) error
	ResumeContainer(ctx context.Context, id string) error
	UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error
	WaitTaskExit(ctx context.Context, id string, pid string) (int32, error)
	SignalTask(ctx context.Context, containerID string, signal syscall.Signal) error
	WinResize(ctx context.Context, containerID string, height, width uint32) error
}

type PedTrait struct {
}

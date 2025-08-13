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

type ContainerTrait interface {
	// RTOS may contains no the concept of PID, so we use a dummy value
	GetAnnotations() map[string]string
	GetPid() int
	ID() string
	Sandbox() SandboxTraits
	TaskInfo() RTOSTask
}

// some of which required by containerd
type SandboxTraits interface {
	Annotation(key string) (string, error)
	SetAnnotations(annotations map[string]string)
	AllAnnotations() map[string]string
	GetAllContainers() []ContainerTrait
	GetContainer(id string) ContainerTrait
	GetNetNamespace() string
	SandboxID() string
	CheckDaemon() *libmica.MicaDaemonState

	// Stats(ctx context.Context) (SandboxStats, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context, force bool) error
	Delete(ctx context.Context) error
	Status() SandboxStatus

	// ContainerManagement
	CreateContainer(ctx context.Context, config ContainerConfig) (ContainerTrait, error)
	DeleteContainer(ctx context.Context, id string) (ContainerTrait, error)
	StartContainer(ctx context.Context, id string) (ContainerTrait, error)
	StopContainer(ctx context.Context, id string, force bool) (ContainerTrait, error)
	KillContainer(ctx context.Context, id string) (ContainerTrait, error)
	StatusContainer(id string) (ContainerState, error)
	StatsContainer(ctx context.Context, id string) (ContainerStats, error)
	WaitContainer(ctx context.Context, id string, pid string) (int32, error)
	IOStream(containerID, processID string) (io.WriteCloser, io.Reader, io.Reader, error)
	GetOOMEvent(ctx context.Context) (string, error)
	// Not supported well
	// TODO: aftet unified micran and micad, we can achive sending signals to RTOS clients
	PauseContainer(ctx context.Context, id string) error
	ResumeContainer(ctx context.Context, id string) error
	UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error
	WaitTaskExit(ctx context.Context, containerID string, taskid uint32) (int32, error)
	SignalTask(ctx context.Context, containerID, processID string, signal syscall.Signal, all bool) error
	WinsizeTask(ctx context.Context, containerID, processID string, height, width uint32) error
}

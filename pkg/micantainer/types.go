package micantainer

// Inspired by kata-containers

import (
	"context"
	"io"
	"syscall"
	"time"

	vc "github.com/kata-containers/kata-containers/src/runtime/virtcontainers"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type MicantainerManager interface {
	CreateSandbox(ctx context.Context, config SandboxConfig, hookFunc func(context.Context) error) (Sandbox, error)
	CleanupContainer(ctx context.Context, sandboxID string, containerID string, force bool) error
}

// NOTICE: `task` represent the process, thread or other task runner in RTOS
type Micantainer interface {
	// RTOS may contains no the concept of PID, so we use a dummy value
	GetPid() int
	ID() string
	Sandbox() Sandbox
	TaskInfo() RTOSTask
}

// some of which required by containerd
type Sandbox interface {
	GetAllContainers() []Micantainer
	GetContainer(id string) Micantainer
	ID() string
	// Stats(ctx context.Context) (SandboxStats, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Delete(ctx context.Context) error
	Monitor(ctx context.Context) (chan error, error)
	Status() SandboxStatus

	// ContainerManagement
	CreateContainer(ctx context.Context, config ContainerConfig) (Micantainer, error)
	DeleteContainer(ctx context.Context, id string) (Micantainer, error)
	StartContainer(ctx context.Context, id string) (Micantainer, error)
	StopContainer(ctx context.Context, id string, force bool) (Micantainer, error)
	KillContainer(ctx context.Context, id string) (Micantainer, error)
	StatusContainer(id string) (ContainerState, error)
	StatsContainer(ctx context.Context, id string) (ContainerStats, error)
	PauseContainer(ctx context.Context, id string) error
	ResumeContainer(ctx context.Context, id string) error
	UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error
	WaitContainer(ctx context.Context, id string, pid string) (int32, error)
	// Not supported well
	// TODO: aftet unified micran and micad, we can achive sending signals to RTOS clients
	SignalProcess(ctx context.Context, containerID, processID string, signal syscall.Signal, all bool) error
	WinsizeProcess(ctx context.Context, containerID, processID string, height, width uint32) error
	IOStream(containerID, processID string) (io.WriteCloser, io.Reader, io.Reader, error)

	AddInterface(ctx context.Context, inf *pbTypes.Interface) (*pbTypes.Interface, error)
	RemoveInterface(ctx context.Context, inf *pbTypes.Interface) (*pbTypes.Interface, error)
	ListInterfaces(ctx context.Context) ([]*pbTypes.Interface, error)

	GetOOMEvent(ctx context.Context) (string, error)
}

// ************** types **************

type Container struct {
	ctr     context.Context
	config  *ContainerConfig
	sandbox *Sandbox
	id      string
	// in dir <sandboxID>/<containerID>
	containerPath string
	mounts        []Mount
	state         ContainerState
}

type ContainerStats struct {
	NetworkStats []*NetworkStats
}

type ContainerState struct {
	State StateString `json:"state"`
}

func (s *ContainerState) Valid() bool {
	return s.State.valid()
}

func (s *ContainerState) ValidTransition(old StateString, new StateString) error {
	return s.State.validTransition(old, new)
}

type ContainerStatus struct {
	Spec        *specs.Spec
	StartedAt   time.Time
	ID          string
	Rootfs      string
	Pid         int
	Annotations map[string]string
}

type RTOSTask struct {
	StartTime time.Time
	// always the shim PID
	Pid int
}

type ContainerConfig struct {
	ID             string
	Rootfs         RootFs
	Mount          []Mount
	ReadOnlyRootfs bool
}

type RootFs struct {
	// Source specifies the path of the rootfs in host filesystem
	Source string
	Target string
	// Target specify where the rootfs is mounted if it has been mounted
	// Type specifies the type of filesystem to mount.
	Type string
	// Options specifies zero or more fstab style mount options.
	Options []string
	// Mounted specifies whether the rootfs has be mounted or not
	Mounted bool
}

type ContainerType string

// List different types of containers
const (
	// PodContainer identifies a container that should be associated with an existing pod
	PodContainer ContainerType = "pod_container"
	// PodSandbox identifies an infra container that will be used to create the pod
	PodSandbox ContainerType = "pod_sandbox"
	SideCar    ContainerType = "side_car"
	// SingleContainer is utilized to describe a container that didn't have a container/sandbox
	// annotation applied. This is expected when dealing with non-pod container (ie, running
	// from ctr, podman, etc).
	SingleContainer ContainerType = "single_container"
	// UnknownContainerType specifies a container that provides container type annotation, but
	// it is unknown.
	UnknownContainerType ContainerType = "unknown_container_type"
)

func (ct ContainerType) IsRegularContainer() bool {
	return ct == SingleContainer
}

// A pod container can not be converted into a sandbox
func (ct ContainerType) CanBeSandbox() bool {
	return ct == PodSandbox || ct == SingleContainer
}

func (ct ContainerType) IsCriSandbox() bool {
	return ct == PodSandbox
}

func From(ct vc.ContainerType) ContainerType {
	var into ContainerType = UnknownContainerType
	switch ct {
	case vc.PodContainer:
		into = PodContainer
	case vc.PodSandbox:
		into = PodSandbox
	case vc.SingleContainer:
		into = SingleContainer
	default:
		into = UnknownContainerType
	}
	return into
}

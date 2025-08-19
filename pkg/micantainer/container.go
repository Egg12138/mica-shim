package micantainer

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	utils "mica-shim/pkg/fileutils"
	"path/filepath"
	"time"

	vc "github.com/kata-containers/kata-containers/src/runtime/virtcontainers"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type ContainerStats struct {
	NetworkStats []*NetworkStats
}

type ContainerState struct {
	Bundle string        `json:"bundle"`
	ID     string        `json:"id"`
	CType  ContainerType `json:"c_type"`
	State  StateString   `json:"state"`
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
	State       ContainerState
	ID          string
	Rootfs      string
	Pid         int
	Annotations map[string]string
}

// RTOSTask represents a task running in the RTOS.
type RTOSTask struct {
	StartTime time.Time
	// TaskID is the identifier of the task, managed by micran.
	TaskID uint32
	// ReceiverAddr is the memory address of the receiver inside the RTOS.
	ReceiverAddr uint64
}


// ContainerConfig holds the configuration for a container.
type ContainerConfig struct {
	ID             string
	Rootfs         RootFs
	Mount          []Mount
	ReadOnlyRootfs bool
	// Pid is typically the shim pid.
	Pid         int
	Annotations map[string]string

	RelativePath string  `json:"relative_path"`
	PedestalType PedType `json:"pedestal_type"`
	PedestalConf string  `json:"pedestal_conf"`
	OS           string  `json:"os"`
	NCpu         int     `json:"ncpu"` // Default = 1

	CpuLimit   int    `json:"cpu_limit"`
	CpusetCpus string `json:"cpuset_cpus"`
	CpuShares  uint64 `json:"cpu_shares"`
	CpuQuota   int64  `json:"cpu_quota"`
	CpuPeriod  uint64 `json:"cpu_period"`

	MemoryLimit       int64   `json:"memory_limit"`
	MemoryReservation int64   `json:"memory_reservation"`
	MemorySwap        int64   `json:"memory_swap"`
	MemoryKernel      int64   `json:"memory_kernel"`
	MemorySwappiness  *uint64 `json:"memory_swappiness"`
	OomKillDisable    bool    `json:"oom_kill_disable"`

	// cpu is the allocated CPU, -1 if not allocated.
	cpu int
}

// RootFs represents the root filesystem of the container.
type RootFs struct {
	// Source is the path to the rootfs on the host.
	Source string
	Target string
	// Type is the filesystem type.
	Type string
	// Options are fstab-style mount options.
	Options []string
	// Mounted indicates whether the rootfs is mounted.
	Mounted bool
}

type ContainerType string

// Defines the different types of containers.
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

// Container represents a single container instance.
type Container struct {
	ctr     context.Context
	config  *ContainerConfig
	sandbox *Sandbox
	id      string
	// containerPath is the path to the container's directory: <sandboxID>/<containerID>.
	containerPath string
	mounts        []Mount
	state         ContainerState
	taskInfo      RTOSTask
}

func (ct ContainerType) IsRegularContainer() bool {
	return ct == SingleContainer
}

// CanBeSandbox checks if the container type can be a sandbox.
// A pod container cannot be converted into a sandbox.
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

// NOTICE: cleanup exclusively
func CleanupContainer(ctx context.Context, sandboxID string, containerID string, force bool) error {
	if sandboxID == "" || containerID == "" {
		return fmt.Errorf("sandboxID or containerID is empty")
	}

	err := cleanupPersistResource(ctx, sandboxID)
	if err != nil {
		return err
	}
	return nil

	// err = cleanupContainer(ctx, sandboxID, containerID, force)
	// if err != nil {
	// 	return err
	// }
}

func cleanupPersistResource(ctx context.Context, sandboxID string) error {
	log.Infof("persist resource is not supported yet")
	return nil
}


// newContainer creates a new container instance.
func newContainer(ctx context.Context, s *Sandbox, cc *ContainerConfig) (*Container, error) {
	container := &Container{
		id:     cc.ID,
		config: cc,
	}

	if !container.validMicaContainer() {
		return nil, fmt.Errorf("invalid mica container: %v", container)
	}

	return container, nil
}

func (c *Container) start(ctx context.Context) error {
	if err := c.checkSandboxRunnig("start"); err != nil {
		return err
	}

	if c.state.State != StateReady && c.state.State != StateStopped {
		return fmt.Errorf("container is not ready or stopped, cannot start")
	}

	if err := startContainerInSandbox(ctx, c.sandbox, c.config); err != nil {
		log.Error("failed to start cotnainer")
		if err := c.stop(ctx, true); err != nil {
			log.Warn("failed to stop the container after start failed")
		}
		return err
	}

	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) checkSandboxRunnig(op string) error {
	if op == "" {
		return fmt.Errorf("operation cannot be empty")
	}
	if c.sandbox.state.State != StateRunning {
		return fmt.Errorf("sandbox is not running, cannot %s container", op)
	}
	return nil
}

func (c *Container) createInSanbox(ctx context.Context) error {

	// TODO:  TOO many works
	rtosTask, err := createContainerInSandbox(ctx, c.sandbox, c.config)
	if err != nil {
		return err
	}

	c.taskInfo = *rtosTask

	if err := c.setContainerState(ctx, StateReady); err != nil {
		return err
	}
	return nil
}

func (c *Container) stop(ctx context.Context, force bool) error {
	if c.state.State == StateStopped {
		log.Infof("Container %s is already stopped", c.id)
		return nil
	}
	if err := c.state.State.validTransition(c.state.State, StateStopped); err != nil {
		return err
	}

	c.kill(ctx, true)
	c.sandbox.agent.waitTask(ctx, c, c.id)
	if err := c.sandbox.agent.stopContainer(ctx, c.sandbox, *c); err != nil && !force{
		return err
	}

	if err := c.setContainerState(ctx, StateStopped); err != nil {
		return err
	}

	return nil
}

func (c *Container) kill(ctx context.Context, force bool) error {
	return nil
}

func (c *Container) delete(ctx context.Context) error {
	if c.state.State != StateReady &&
		c.state.State != StatePaused &&
		c.state.State != StateStopped {
		return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete container")
	}

	if err := c.sandbox.removeContainer(c.id); err != nil {
		return err
	}

	return c.sandbox.StoreSandbox(ctx)
}

func (c *Container) ID() string {
	return c.id
}

func (c *Container) GetAnnotations() map[string]string {
	return c.config.Annotations
}

func (c *Container) GetPid() int {
	return c.config.Pid
}

func (c *Container) GetMemoryLimit() uint64 {
	return uint64(c.config.MemoryLimit)
}

func (c *Container) Sandbox() SandboxTraits {
	return c.sandbox
}

func (c *Container) TaskInfo() RTOSTask {
	return c.taskInfo
}

func (c *Container) Status() StateString {
	return c.state.State
}

func (c *Container) State() *ContainerState {
	return &c.state
}

func validOS(os string) bool {
	ret := inList(defs.PreservedOS[:], os)
	return ret
}

func validFirmware(root, firmware string) bool {
	// firmware path: <bundle>/rootfs/<firmware>
	resolved, err := utils.ResolvePath(filepath.Join(root, firmware))
	if err != nil {
		return false
	}
	ret := utils.FileExist(resolved)
	return ret
}

func validCompatibility(_ *ContainerConfig) bool {
	// TODO: needed to ? how to check compatibility?
	return true
}

// NOTICE: Xen is the only supported ped for now
func (c *Container) validMicaContainer() bool {

	osValid := validOS(c.config.GetOS())
	fwValid := validFirmware(c.containerPath, c.config.GetFirmwarePath())
	compatValid := validCompatibility(c.config)
	judge := osValid && fwValid && compatValid
	log.Debugf(`
		validMicaContainer:
		osValid = %v,
		fwValid = %v,
		compatValid = %v,
		judge = %v
	`, osValid, fwValid, compatValid, judge)

	return judge
}

func (c *Container) setContainerState(ctx context.Context, state StateString) error {
	if state == "" {
		return fmt.Errorf("state cannot be empty")
	}

	log.Debugf("set container state from %s to %s", c.state.State, state)
	c.state.State = state
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		log.Errorf("save sandbox state failed")
		return err
	}
	return nil
}

func (c *Container) allocClientCPU() error {
	// Use the container-specific CPU limit instead of the global HostMaxCPU.
	cpu, err := allocCPUWithLimit(c.config.NCpu, c.config)
	if err != nil {
		return err
	}
	c.config.cpu = cpu
	return nil
}

func allocCPUWithLimit(ncpu int, config *ContainerConfig) (int, error) {
	if ncpu < 1 {
		return 0, fmt.Errorf("ncpu must be at least 1")
	}

	maxCPU := getContainerCPULimit(config)
	if ncpu > maxCPU {
		return 0, fmt.Errorf("requested ncpu %d exceeds container CPU limit %d", ncpu, maxCPU)
	}

	// Handle cpuset.cpus if specified
	if config != nil && config.CpusetCpus != "" {
		// For now, log the cpuset requirement but use simple allocation
		// TODO: Implement proper cpuset.cpus parsing and allocation
		log.Infof("Container specifies cpuset.cpus: %s", config.CpusetCpus)
	}

	// Simple round-robin allocation based on current time within the allowed range
	// TODO: let containerd the manager CPU selector, and limit the CPU perspective
	allocatedCPU := int(time.Now().UnixNano()) % maxCPU

	log.Debugf("Allocated CPU %d for ncpu=%d (container limit: %d)", allocatedCPU, ncpu, maxCPU)
	return allocatedCPU, nil
}

// getContainerCPULimit returns the effective CPU limit for a container,
// considering both OCI spec limits and system constraints.
func getContainerCPULimit(cfg *ContainerConfig) int {
	// TODO: The runtime cannot detect the max number of CPUs Xen can handle.
	systemCPUs := maxCPUNumber()

	// Use the container-specific CPU limit from the OCI spec, if available.
	if cfg != nil {
		log.Debugf(`cpu config:
		cpuLimit: %d, 
		cpuPeriod: %d, 
		cpuQuota: %d, 
		cpuShares: %d, 
		cpusetCpus: %s, 
		`, cfg.CpuLimit, cfg.CpuPeriod, cfg.CpuQuota, cfg.CpuShares, cfg.CpusetCpus)

	}
	if cfg != nil && cfg.CpuLimit > 0 {
		return min(cfg.CpuLimit, int(systemCPUs))
	}

	// As a fallback, use all available CPUs, but reserve one for the host.
	defaultLimit := int(systemCPUs)
	if defaultLimit > 1 {
		defaultLimit -= 1
	}

	return defaultLimit
}

func (c *Container) GetClientCPU() (int, error) {
	if c.config.cpuUnset() {
		if err := c.allocClientCPU(); err != nil {
			return c.config.cpu, err
		}
	}
	return c.config.cpu, nil
}

func (c *Container) SaveState() error {
	failed, failed1 := false, false
	var err error
	var err1 error
	st := c.State()
	stateInBundle := filepath.Join(c.containerPath, defs.MicantainerStateFile)
	stateInMicranDir := filepath.Join(defs.MicranStateDir, c.id, defs.MicantainerStateFile)

	if err = utils.SaveStructToFile(stateInBundle, st); err != nil {
		failed = true
		err = fmt.Errorf("failed to save state to <%s>: %w", stateInBundle, err)
	}

	if err1 = utils.SaveStructToFile(stateInMicranDir, st); err1 != nil {
		failed1 = true
		err1 = fmt.Errorf("failed to save state to <%s>: %w", stateInMicranDir, err1)
	}

	if failed1 && failed {
		return fmt.Errorf("failed to save container state to both locations: %w, %w", err, err1)
	}
	return nil
}

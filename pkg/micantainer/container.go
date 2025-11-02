// Package micantainer implements the core logic for managing sandboxes and containers.
package micantainer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	defs "mica-shim/definitions"
	er "mica-shim/errors"
	log "mica-shim/logger"
	"mica-shim/pkg/libmica"
	ped "mica-shim/pkg/pedestal"
	utils "mica-shim/pkg/utils"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/containerd/errdefs"
	"github.com/hashicorp/go-multierror"
	vc "github.com/kata-containers/kata-containers/src/runtime/virtcontainers"
	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/cpuset"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

// ContainerStats holds statistics for a container.
type ContainerStats struct {
	ResourceStats *ResourceStats
	NetworkStats  []*NetworkStats
}

// ResourceStats holds CPU and memory statistics.
type ResourceStats struct {
	CPUStats    CPUStats    `json:"cpu_stats,omitempty"`
	MemoryStats MemoryStats `json:"memory_stats,omitempty"`
}

// CPUStats holds CPU usage statistics.
type CPUStats struct {
	// TotalUsage is the total physical CPU time spent on the current container.
	// In cgroup metrics, CPUStat includes UserUsec and SystemUsec, but it's
	// unnecessary for an RTOS to calculate them separately.
	TotalUsage uint64 `json:"total_usage,omitempty"`
	// NrPeriods is the number of schedule cycles after the client is created,
	// if the pedestal supports it.
	NrPeriods uint64 `json:"nr_periods,omitempty"`
}

// MemoryStats holds memory usage statistics.
type MemoryStats struct {
	Cache uint64            `json:"cache"`
	Usage MemoryEntry       `json:"usage"`
	Stats map[string]uint64 `json:"stats"`
}

// MemoryEntry holds detailed memory usage data.
type MemoryEntry struct {
	Failcnt uint64 `json:"failcnt,omitempty"`
	Limit   uint64 `json:"limit,omitempty"`
	// MaxEver is the maximum memory usage recorded. In static allocation, MaxEver equals Limit.
	MaxEver uint64 `json:"max_ever,omitempty"`
	Usage   uint64 `json:"usage,omitempty"`
}

// ContainerState represents the state of a container.
type ContainerState struct {
	Bundle string
	ID     string
	CType  ContainerType
	State  StateString
}

// Valid checks if the container state is valid.
func (s *ContainerState) Valid() bool {
	return s.State.valid()
}

// ValidTransition checks if a state transition is valid.
func (s *ContainerState) ValidTransition(old StateString, new StateString) error {
	return s.State.validTransition(old, new)
}

// ContainerStatus represents the status of a container.
type ContainerStatus struct {
	Spec        *specs.Spec
	CreatedAt   time.Time
	State       ContainerState
	ID          string
	Rootfs      string
	Pid         int // The shim pid.
	Annotations map[string]string
}

// RTOSTask represents a task running in the RTOS.
type RTOSTask struct {
	CreateTime time.Time
	// TaskID is the identifier of the task, managed by micran.
	TaskID string
	// ReservedAddr is the memory address of the reserved region entry inside the RTOS.
	ReservedAddr uint64
}

// ContainerConfig holds the configuration for a container.
type ContainerConfig struct {
	ID             string
	Rootfs         RootFs
	Mount          []Mount
	ReadOnlyRootfs bool
	IsInfra        bool
	Pid            int // Pid is typically the shim pid.
	Annotations    map[string]string
	Resources      *specs.LinuxResources

	// ElfAbsPath is the absolute path of the <os>.elf in the host.
	ElfAbsPath   string      `json:"elf_abs_path"`
	PedestalType ped.PedType `json:"pedestal_type"`
	PedestalConf string      `json:"pedestal_conf"`
	OS           string      `json:"os"`

	// CpuLimit is the CPU limit in cores (cpuqupta / cpuperiod).
	CpuLimit  uint32 `json:"cpu_limit"`
	CpuQuota  int64  `json:"cpu_quota"`
	CpuPeriod uint64 `json:"cpu_period"`
	// CpusetCpus is the set of physical CPUs the client is allowed to use (e.g., "1,3-5").
	CpusetCpus string `json:"cpuset_cpus"`
	// CpuShares is the relative weight of the container for CPU time.
	CpuShares uint64 `json:"cpu_shares"`
	// VCPUNum is the number of virtual CPUs. Equals CpuLimit if not pinning; otherwise, equals the size of the cpuset.
	VCPUNum uint32 `json:"vcpu_num"`
	// PCPUNum is the number of allocated physical CPUs.
	// TODO: Implement for openAMP and Jailhouse cases.
	PCPUNum int `json:"ncpu"`

	// MemoryLimitMB is the memory limit in MiB.
	MemoryLimitMB uint32 `json:"memory_limit"`
	// MemoryMinMB is the initial memory in MiB assigned at client boot.
	MemoryMinMB         uint32  `json:"memory_min"`
	MemoryReservationMB uint32  `json:"memory_reservation"`
	MemorySwapMB        uint32  `json:"memory_swap"`
	MemoryKernelMB      uint32  `json:"memory_kernel"`
	MemorySwappinessMB  *uint32 `json:"memory_swappiness"`
	OomKillDisable      bool    `json:"oom_kill_disable"`

	// Cmdline is the boot command line for the guest.
	Cmdline string `json:"cmdline"`
}

// Noop writer/reader are used for infra container which never has PTY or IO.
type noopWriteCloser struct{}

func (noopWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (noopWriteCloser) Close() error {
	return nil
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

// ContainerType is a string representing the type of a container.
type ContainerType string

// Defines the different types of containers.
const (
	// PodContainer identifies a container that should be associated with an existing pod.
	PodContainer ContainerType = "pod_container"
	// PodSandbox identifies an infra container that will be used to create a pod.
	PodSandbox ContainerType = "pod_sandbox"
	// SideCar identifies a sidecar container.
	SideCar ContainerType = "side_car"
	// SingleContainer is utilized to describe a container that doesn't have a container/sandbox
	// annotation applied. This is expected when dealing with non-pod containers (e.g., from ctr, podman).
	SingleContainer ContainerType = "single_container"
	// UnknownContainerType specifies a container that provides a container type annotation, but it is unknown.
	UnknownContainerType ContainerType = "unknown_container_type"
)

// Container represents a single container instance, encapsulating its configuration,
// state, and relationship with a sandbox.
type Container struct {
	ctx           context.Context
	me            libmica.MicaExecutor
	config        *ContainerConfig
	id            string
	sandbox       *Sandbox
	sandboxId     string
	mounts        []Mount
	rootfs        RootFs
	containerPath string // The path relative to the root bundle: <sandboxID>/<containerID>.
	state         ContainerState
	taskInfo      RTOSTask
	exitNotifier  chan struct{}
	exitOnce      sync.Once
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

// From converts a virtcontainers.ContainerType to a micantainer.ContainerType.
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

// loadSandbox restores a sandbox from disk by its ID.
func loadSandbox(ctx context.Context, id string) (sandbox *Sandbox, err error) {
	if id == "" {
		return nil, er.EmptySandboxID
	}

	log.Debugf("Trying to restore sandbox from disk.")
	ss, err := RestoreSandbox(ctx, id)
	if err != nil {
		log.Debugf("Failed to restore sandbox from disk: %v.", err)
		return nil, err
	}
	c := ss.Config

	sandbox, err = createSandbox(ctx, &c)
	if err != nil {
		log.Errorf("Failed to create sandbox: %v.", err)
		return nil, err
	}

	if err := sandbox.loadContainersToSandbox(ctx); err != nil {
		return nil, err
	}
	return sandbox, nil
}

// CleanupContainer stops and deletes a container and its associated sandbox if it's the last one.
// NOTICE: This function is designed for exclusive cleanup operations.
func CleanupContainer(ctx context.Context, sandboxID string, containerID string, force bool) error {
	log.Debugf("Cleaning up sandbox %s, container %s.", sandboxID, containerID)
	if sandboxID == "" {
		return er.EmptySandboxID
	}

	if containerID == "" {
		return er.EmptyContainerID
	}

	sandbox, err := loadSandbox(ctx, sandboxID)
	if err != nil {
		if err == er.SandboxNotFound {
			if !libmica.ClientNotExist(containerID) && !force {
				return fmt.Errorf("sandbox state missing while client %s still exists", containerID)
			}
			log.Debugf("Sandbox %s already removed from disk, skipping container %s cleanup.", sandboxID, containerID)
			return nil
		}
		return err
	}

	if _, err = sandbox.StopContainer(ctx, containerID, force); err != nil {
		if err != er.ContainerNotFound && !force {
			return err
		}
		log.Debugf("Container %s already stopped or absent in sandbox %s: %v.", containerID, sandboxID, err)
	}

	if _, err = sandbox.DeleteContainer(ctx, containerID); err != nil {
		if err != er.ContainerNotFound && !force {
			return err
		}
		log.Debugf("Container %s already deleted from sandbox %s: %v.", containerID, sandboxID, err)
	}

	if len(sandbox.containers) > 0 {
		return nil
	}

	if err = sandbox.Stop(ctx, force); err != nil && !force {
		return err
	}

	if err = sandbox.Delete(ctx); err != nil {
		return err
	}

	return nil
}

// newContainer creates a new container struct instance.
// It assumes that the container config is already parsed.
func newContainer(ctx context.Context, s *Sandbox, cc *ContainerConfig) (*Container, error) {
	if cc == nil {
		return &Container{}, fmt.Errorf("container config is none")
	}

	if cc.ID == "" {
		log.Debugf("Empty container id.")
		return &Container{}, er.EmptyContainerID
	}

	c := &Container{
		id:            cc.ID,
		me:            libmica.MicaExecutor{Id: cc.ID},
		sandbox:       s,
		sandboxId:     s.id,
		config:        cc,
		rootfs:        cc.Rootfs,
		containerPath: filepath.Join(s.id, cc.ID),
		mounts:        cc.Mount,
		state:         ContainerState{State: StateDown},
		taskInfo:      RTOSTask{},
		ctx:           s.ctx,
	}

	if err := c.RestoreState(); err != nil {
		log.Warnf("Failed to restore container state: %v.", err)
	}

	c.updateExitNotifier(c.checkState())

	return c, nil
}

// start begins the execution of the container.
func (c *Container) start(ctx context.Context) error {
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}

	if c.config != nil && c.config.IsInfra {
		if currentState == StateRunning {
			return nil
		}
		if currentState != StateReady && currentState != StateStopped {
			return fmt.Errorf("container is not ready or stopped, cannot start")
		}
		if err := c.state.ValidTransition(currentState, StateRunning); err != nil {
			return err
		}
		return c.setContainerState(ctx, StateRunning)
	}

	if currentState == StateRunning {
		return fmt.Errorf("container %s is already running", c.id)
	}

	if currentState != StateReady && currentState != StateStopped {
		return fmt.Errorf("container is not ready or stopped, cannot start")
	}

	if err := c.state.ValidTransition(currentState, StateRunning); err != nil {
		return err
	}

	if err := startClient(ctx, c.sandbox, c); err != nil {
		log.Warnf("Failed to start container: %v, stopping it", err)
		if err := c.stop(ctx, true); err != nil {
			log.Warn("Failed to stop the container after start failed.")
		}
		return err
	}

	return c.setContainerState(ctx, StateRunning)
}

// create prepares the container to be started.
func (c *Container) create(ctx context.Context) error {
	if c.config != nil && c.config.IsInfra {
		c.taskInfo = RTOSTask{
			CreateTime: time.Now(),
			TaskID:     c.id,
		}
		return c.setContainerState(ctx, StateReady)
	}

	rtosTask, err := initContainerTaskInSandbox(c.sandbox, c.config)
	if err != nil {
		return err
	}

	c.taskInfo = *rtosTask

	if _, err := c.ensureClientPresence(); err != nil {
		return err
	}

	if err := c.setContainerState(ctx, StateReady); err != nil {
		return err
	}
	return nil
}

// doStop performs the actual stop operation on the client.
func (c *Container) doStop(force bool) error {
	if c.config != nil && c.config.IsInfra {
		return nil
	}
	currentState := c.checkState()
	if currentState == StateStopped {
		log.Debugf("Container %s is already stopped.", c.id)
		return nil
	}

	if err := c.state.ValidTransition(currentState, StateStopped); err != nil && !force {
		return err
	}

	if err := libmica.Stop(c.ID()); err != nil {
		return err
	}
	return nil
}

// stop stops the container.
func (c *Container) stop(ctx context.Context, force bool) error {
	if _, err := c.ensureClientPresence(); err != nil {
		return err
	}

	var err error
	if err = c.doStop(force); err != nil {
		log.Debugf("+++++ failed to stop contaienr %s: %v", c.id, err)
		return err
	}
	log.Debugf("+++++ stopped contaienr %s", c.id)

	if err = c.setContainerState(ctx, StateStopped); err != nil {
		return err
	}

	return nil
}

// kill forcibly stops the container.
// Due to the 1:1:1 relationship of Container:ClientOS:Task in mica, kill() is essentially stop().
func (c *Container) kill() error {

	if c.sandbox.state.State != StateReady && c.sandbox.state.State != StateRunning {
		return fmt.Errorf("sandbox is not running or ready, can not signal container")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	log.Debugf("Container state is %s.", currentState)

	if libmica.ClientNotExist(c.id) {
		return c.setContainerState(c.ctx, StateStopped)
	} else if err := c.doStop(true); err != nil {
		log.Debugf("+++++ failed to stop contaienr %s", c.id)
		return err
	}
	log.Debugf("+++++ stopped contaienr %s", c.id)

	if err := c.setContainerState(c.ctx, StateStopped); err != nil {
		return err
	}
	return nil
}

// delete removes the container.
// This differs from mica, where `rm` forces a client stop. For a container engine, that is bad practice.
func (c *Container) delete(ctx context.Context) error {
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StateReady &&
		currentState != StatePaused &&
		currentState != StateStopped {
		return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete container")
	}

	if c.config == nil || !c.config.IsInfra {
		if err := libmica.Remove(c.id); err != nil {
			log.Debugf("Failed to remove container %s.", err)
			return err
		}
	}
	if err := c.sandbox.removeContainer(c.id); err != nil {
		return err
	}
	return c.sandbox.StoreSandbox(ctx)
}

// pause pauses the container's execution.
func (c *Container) pause(ctx context.Context) error {
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StateRunning {
		return fmt.Errorf("container is not running, cannot pause container")
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StatePaused)
	}
	if err := libmica.Pause(c.id); err != nil {
		return er.MicadOpFailed
	}
	return c.setContainerState(ctx, StatePaused)
}

// resume resumes a paused container.
func (c *Container) resume(ctx context.Context) error {
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StatePaused && c.sandbox.state.State != StateStopped {
		return fmt.Errorf("container is not paused, cannot resume container")
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StateRunning)
	}
	log.Debugf("Micran restart a client os, acting as `resume`.")
	if err := libmica.Start(c.id); err != nil {
		return er.MicadOpFailed
	}
	return c.setContainerState(ctx, StateRunning)
}

// update modifies the container's resources.
// TODO: Implement container resource update.
func (c *Container) update(ctx context.Context, resources specs.LinuxResources) error {
	if c.config != nil && c.config.IsInfra {
		return nil
	}
	if c.sandbox.state.State != StateRunning {
		return fmt.Errorf("sandbox is not running, cannot stats container")
	}

	if c.notOperational() {
		return fmt.Errorf("container not ready or running, impossible to update the container")
	}

	res := c.config.Resources
	pedRes := ped.InitResource()
	if res.CPU == nil {
		c.config.Resources.CPU = resources.CPU
	}

	if cpu := resources.CPU; cpu != nil {
		period := cpu.Period
		quota := cpu.Quota
		Cpus := cpu.Cpus
		Mems := cpu.Mems

		if period != nil && *period != 0 {
			res.CPU.Period = period
			*pedRes.CpuPeriod = *period
		}

		if quota != nil && *quota != 0 {
			res.CPU.Quota = quota
			*pedRes.CpuQuota = *quota
		}

		if Cpus != "" {
			res.CPU.Cpus = Cpus
			pedRes.ClientCpuSet = Cpus
		}

		if Mems != "" {
			res.CPU.Mems = Mems
		}
	}

	if res.Memory == nil {
		c.config.Resources.Memory = resources.Memory
	}

	if mem := resources.Memory; mem != nil && mem.Limit != nil {
		c.config.Resources.Memory.Limit = mem.Limit
		*pedRes.MemoryLimitMB = uint32(*mem.Limit >> 20)
	}

	if err := c.sandbox.updateResources(ctx); err != nil {
		return err
	}

	return updateContainerResource(c, pedRes)
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
	return uint64(c.config.MemoryLimitMB)
}

func (c *Container) Sandbox() SandboxTraits {
	return c.sandbox
}

func (c *Container) TaskInfo() RTOSTask {
	return c.taskInfo
}

func (c *Container) Status() StateString {
	return c.checkState()
}

func (c *Container) State() *ContainerState {
	c.checkState()
	return &c.state
}

// Signal sends a signal to the container.
// TODO: Implement a POSIX signals hub.
func (c *Container) Signal(ctx context.Context, signal syscall.Signal) error {
	if c.sandbox.notOperational() {
		return fmt.Errorf("sandbox is not running or ready, can not signal container")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StateRunning && currentState != StateReady && currentState != StatePaused {
		return fmt.Errorf("client os is not running, ready or paused, can not signal container")
	}

	log.Errorf("Container signal is not implemented.")
	return errdefs.ErrNotImplemented
}

// validOS checks if the OS is in the list of preserved OSes.
func validOS(os string) bool {
	ret := utils.InList(defs.PreservedOS[:], os)
	return ret
}

// validComponent checks if a component file is a regular file.
func validComponent(component string) bool {
	log.Debugf("File %s exist: %v.", component, utils.FileExist(component))
	log.Debugf("File %s is regular: %v.", component, utils.IsRegular(component))
	if !utils.IsRegular(component) {
		return false
	}

	hostArch := runtime.GOARCH

	if match, _ := utils.IsELFForHost(component); match {
		return true
	}

	// check for arm64 xen client image
	if hostArch == "arm64" {
		if fh, err := os.Open(component); err == nil {
			defer fh.Close()
			buf := make([]byte, 0x40)
			if n, _ := fh.Read(buf); n >= 0x3C {
				if bytes.Contains(buf, []byte("ARMd")) {
					return true
				}
			}
		}
	}

	return true
}

// validFirmware checks if the firmware file is valid.
func validFirmware(firmware string) bool {
	return validComponent(firmware)
}

// validBinfile checks if the binary file is valid.
// For Xen, this is typically image.bin.
func validBinfile(binpath string) bool {
	return validComponent(binpath)
}

// validMicaContainer checks if the container configuration is valid for mica.
// NOTICE: Xen is the only supported pedestal for now.
func (c *Container) validMicaContainer() bool {
	if c.config != nil && c.config.IsInfra {
		return true
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Errorf("failed to get current working directory: %v", err)
		return false
	}

	log.Debugf("current working dir: %s", cwd)
	log.Debugf("current env variable $PATH: %s", os.Getenv("PATH"))
	mCpuNum := ped.MaxCPUNum()
	log.Debugf("*******************max machine cpu number: %d", mCpuNum)
	osValid := validOS(c.GetOS())
	fwValid := validFirmware(c.GetFirmwarePath())
	if HostPedType == ped.Xen {
		binFile := validBinfile(c.GetPedestalConf())
		fwValid = binFile && fwValid
	}
	judge := osValid && fwValid
	log.Debugf(`
	validMicaContainer:
		osValid = %v,
		fwValid = %v,
		judge = %v
	`, osValid, fwValid, judge)

	return judge
}

// setContainerState updates the container's state and persists it.
func (c *Container) setContainerState(ctx context.Context, state StateString) error {
	if state == "" {
		return fmt.Errorf("state cannot be empty")
	}

	log.Debugf("Set container state from %s to %s.", c.state.State, state)
	c.state.State = state
	c.updateExitNotifier(state)
	if err := c.SaveState(); err != nil {
		log.Errorf("failed to save container state: %v", err)
		return err
	}
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		log.Errorf("failed to save sandbox state: %v", err)
		return err
	}
	return nil
}

func (c *Container) checkState() StateString {
	if c == nil && c.id == "" {
		return StateDown
	}

	if c.config != nil && c.config.IsInfra {
		return c.state.State
	}

	if libmica.ClientNotExist(c.id) {
		if c.state.State != StateDown {
			if err := c.setContainerState(c.ctx, StateDown); err != nil {
				log.Warnf("failed to mark container %s as down: %v", c.id, err)
			}
		}
		return StateDown
	}

	return c.state.State
}

func (c *Container) ensureClientPresence() (StateString, error) {
	state := c.checkState()
	if state != StateDown {
		return state, nil
	}

	if err := c.registerClientWithMicad(); err != nil {
		return StateDown, err
	}

	state = c.checkState()
	if state == StateDown {
		return StateDown, er.ContainerNotFound
	}

	return state, nil
}

func (c *Container) registerClientWithMicad() error {
	if c == nil || c.config == nil || c.config.IsInfra {
		return nil
	}

	if !libmica.ClientNotExist(c.id) {
		return nil
	}

	conf, err := createMicaClientConf(c)
	if err != nil {
		return err
	}

	if err := libmica.Create(conf); err != nil {
		return err
	}

	return c.setContainerState(c.ctx, StateReady)
}

const num2CapRatio = 100

// getContainerCPULimit returns the effective CPU limit for a container.
func (cfg *ContainerConfig) getContainerCPULimit() int {
	// TODO: The runtime cannot detect the max number of CPUs Xen can handle.
	systemCPUs := machineCPUNumber()

	if systemCPUs <= 1 {
		return num2CapRatio * 1
	}

	if cfg != nil {
		log.Debugf(`cpu config:
		cpuLimit: %d%,
		cpuPeriod: %d,
		cpuQuota: %d,
		cpuShares: %d,
		cpusetCpus: %s,
		`, cfg.CpuLimit, cfg.CpuPeriod, cfg.CpuQuota, cfg.CpuShares, cfg.CpusetCpus)
	}
	if cfg != nil && cfg.CpuLimit > 0 {
		return min(int(cfg.CpuLimit), int(num2CapRatio*systemCPUs))
	}

	// As a fallback, use all available CPUs, but reserve one for the host.
	defaultLimit := int(systemCPUs)
	if defaultLimit > 1 {
		defaultLimit -= 1
	}

	return defaultLimit
}

func (c *Container) GetClientCPU() (string, error) {
	if c.cpuUnset() {
		return "", nil
	}
	return c.config.CpusetCpus, nil
}

// SaveState persists the container's state to disk at two locations for redundancy.
func (c *Container) SaveState() error {
	serializable := struct {
		ID            string          `json:"id"`
		SandboxID     string          `json:"sandbox_id"`
		State         ContainerState  `json:"state"`
		Config        ContainerConfig `json:"config"`
		TaskInfo      RTOSTask        `json:"task_info"`
		Mounts        []Mount         `json:"mounts"`
		ContainerPath string          `json:"container_path"`
	}{
		ID:            c.id,
		SandboxID:     c.sandboxId,
		State:         c.state,
		Config:        *c.config,
		TaskInfo:      c.taskInfo,
		Mounts:        c.mounts,
		ContainerPath: c.containerPath,
	}

	failed, failed1 := false, false
	var err error
	var err1 error

	cwd, err := os.Getwd()
	if err != nil {
		log.Warnf("Failed to get current working directory: %v", err)
		cwd = "."
	}
	stateInBundle := filepath.Join(cwd, c.containerPath, defs.MicantainerStateFile)
	stateInMicranDir := filepath.Join(defs.MicranContainerStateDir, c.containerPath, defs.MicantainerStateFile)
	log.Infof("stateInBundle: %s", stateInBundle)

	bundleDir := filepath.Dir(stateInBundle)
	if err := utils.EnsureDir(bundleDir, defs.DirMode); err != nil {
		log.Warnf("Failed to ensure bundle directory: %v.", err)
	}
	if err := utils.EnsureDir(filepath.Dir(stateInMicranDir), defs.DirMode); err != nil {
		log.Warnf("Failed to ensure micran state directory: %v.", err)
	}

	if err = utils.SaveStructToJSON(stateInBundle, serializable); err != nil {
		failed = true
		err = fmt.Errorf("failed to save state to <%s>: %w", stateInBundle, err)
	}

	if err1 = utils.SaveStructToJSON(stateInMicranDir, serializable); err1 != nil {
		failed1 = true
		err1 = fmt.Errorf("failed to save state to <%s>: %w", stateInMicranDir, err1)
	}

	if failed1 && failed {
		return fmt.Errorf("failed to save container state to both locations: %w, %w", err, err1)
	}
	return nil
}

// RestoreState loads the container's state from disk, trying the primary and fallback locations.
func (c *Container) RestoreState() error {
	type ContainerStorage struct {
		ID            string          `json:"id"`
		SandboxID     string          `json:"sandbox_id"`
		State         ContainerState  `json:"state"`
		Config        ContainerConfig `json:"config"`
		TaskInfo      RTOSTask        `json:"task_info"`
		Mounts        []Mount         `json:"mounts"`
		ContainerPath string          `json:"container_path"`
	}

	var storage ContainerStorage

	stateInMicranDir := filepath.Join(defs.MicranContainerStateDir, c.id, defs.MicantainerStateFile)
	raw, err := utils.RestoreStructFromJSON(stateInMicranDir)

	if err != nil {
		cwd, err := os.Getwd()
		if err != nil {
			log.Warnf("Failed to get current working directory: %v", err)
			cwd = "."
		}
		stateInBundle := filepath.Join(cwd, c.containerPath, defs.MicantainerStateFile)
		raw, err = utils.RestoreStructFromJSON(stateInBundle)
		if err != nil {
			return fmt.Errorf("failed to restore container state from both locations: %w", err)
		}
	}

	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("failed to marshal raw data: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, &storage); err != nil {
		return fmt.Errorf("failed to unmarshal container storage: %w", err)
	}

	c.state = storage.State
	c.taskInfo = storage.TaskInfo
	c.mounts = storage.Mounts
	c.containerPath = storage.ContainerPath
	c.updateExitNotifier(c.state.State)

	return nil
}

// stats returns the container statistics.
// TODO: Extend the range of stats collected.
func (c *Container) stats() (*ContainerStats, error) {
	if c.sandbox.state.State != StateRunning {
		return nil, fmt.Errorf("sandbox is not running, cannot stats container")
	}
	st := &ContainerStats{}
	return st, nil
}

// wait4exit waits for the container's task to exit.
// TODO: For now, taskId is always a dummy because one client has one task.
// TALK: Is it possible to apply a new task to the client OS in the future, perhaps via Xen?
func (c *Container) wait4exit() (int32, error) {
	currentState := c.checkState()
	if currentState == StateStopped {
		return ok0, nil
	}
	if c.notOperational() && currentState != StatePaused {
		return ok0, errors.New("container is not ready or running, cannot wait for exit")
	}
	return ok0, nil
}

// setVcpuAffinity sets the VCPU affinity for the container.
func (c *Container) setVcpuAffinity(cpuSet cpuset.CPUSet) error {
	var result *multierror.Error
	cpulist := cpuSet.ToSlice()
	if err := c.me.VcpuPin(cpulist); err != nil {
		result = multierror.Append(result, err)
	}

	ret := result.ErrorOrNil()
	if ret == nil {
		c.config.VCPUNum = uint32(cpuSet.Size())
		c.config.CpusetCpus = cpuSet.String()
		c.config.PCPUNum = int(c.config.VCPUNum)
	}
	return ret
}

// ioStream returns the IO streams for the container.
func (c *Container) ioStream(taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	if c.config != nil && c.config.IsInfra {
		return noopWriteCloser{}, bytes.NewReader(nil), bytes.NewReader(nil), nil
	}
	if c.notOperational() {
		return nil, nil, nil, fmt.Errorf("container not ready or running, impossible to signal the container")
	}

	stream := newIOStream(c.sandbox, c, taskID)

	return stream.stdin(), stream.stdout(), stream.stderr(), nil
}

// winresize resizes the container's PTY.
// TODO: Resize the terminal connected to /dev/ttyRPMSG*.
func (c *Container) winresize(height, width uint32) error {
	if c.notOperational() {
		return fmt.Errorf("container not ready or running, impossible to resize the container pty")
	}
	log.Debugf("Resize pty -> %dx%d.", width, height)
	return nil
}

// firmware is the elf file of rtos
func (c *Container) GetFirmwarePath() string {
	return c.config.ElfAbsPath
}

func (c *Container) GetPedestalConf() string {
	return c.config.PedestalConf
}

func (c *Container) GetOS() string {
	return c.config.OS
}

func (c *Container) cpuUnset() bool {
	return c.config.CpusetCpus == ""
}

func (c *Container) GetPedGuestBootBin() string {
	if HostPedType == ped.Xen {
		return c.config.PedestalConf
	}
	return ""
}

func (c *Container) GetPedestalType() ped.PedType {
	return c.config.PedestalType
}

func (c *Container) updateExitNotifier(state StateString) {
	switch state {
	case StateStopped:
		if c.exitNotifier != nil {
			c.exitOnce.Do(func() {
				close(c.exitNotifier)
			})
			c.exitNotifier = nil
		}
	default:
		if c.exitNotifier == nil {
			c.exitNotifier = make(chan struct{})
		}
		c.exitOnce = sync.Once{}
	}
}

// notOperational checks if the container is not in a state to be operated on.
func (c *Container) notOperational() bool {
	currentState := c.checkState()
	return currentState != StateReady && currentState != StateRunning
}

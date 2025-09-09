package micantainer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	er "mica-shim/pkg/errors"
	utils "mica-shim/pkg/fileutils"
	"mica-shim/pkg/libmica"
	ped "mica-shim/pkg/pedestal"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/containerd/errdefs"
	vc "github.com/kata-containers/kata-containers/src/runtime/virtcontainers"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

type ContainerStats struct {
	NetworkStats []*NetworkStats
}

type ContainerState struct {
	Bundle string
	ID     string
	CType  ContainerType
	State  StateString
}

func (s *ContainerState) Valid() bool {
	return s.State.valid()
}

func (s *ContainerState) ValidTransition(old StateString, new StateString) error {
	return s.State.validTransition(old, new)
}

type ContainerStatus struct {
	Spec      *specs.Spec
	StartedAt time.Time
	State     ContainerState
	ID        string
	Rootfs    string
	// shim pid
	Pid         int
	Annotations map[string]string
}

// RTOSTask represents a task running in the RTOS.
type RTOSTask struct {
	StartTime time.Time
	// TaskID is the identifier of the task, managed by micran.
	TaskID string
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
	OciSpec        *specs.Spec

	// relative path of <os>.elf in bundle
	ElfPath      string      `json:"relative_path"`
	PedestalType ped.PedType `json:"pedestal_type"`
	PedestalConf string      `json:"pedestal_conf"`
	OS           string      `json:"os"`
	NCpu         int         `json:"ncpu"` // Default = 1

	// cpuqupta / cpuperiod = cpus: f64 => CPUCapacity = cpus * 100, 
	CpuLimit   int    `json:"cpu_limit"`
	CpuQuota   int64  `json:"cpu_quota"`
	CpuPeriod  uint64 `json:"cpu_period"`
	// host cpu set available for container 
	// => CPU, i.e. the phyical cpu set allowed client to use; format="1,3-5"
	CpusetCpus string `json:"cpuset_cpus"`
	// default to be 1024, CpuShared : 1024 = related a weight 
	// => CPUWeight(1-65535, default=256), in xen domain default weight is 256
	CpuShares  uint64 `json:"cpu_shares"`
	// VCPU
	VPUNum         int `json:"vcpu_num"`

	// Memory in MiB
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
	ctx context.Context

	config *ContainerConfig
	id     string

	sandbox   *Sandbox
	sandboxId string

	mounts []Mount
	rootfs RootFs
	// containerPath is the path relative to the root bundle: <sandboxID>/<containerID>.
	containerPath string

	state    ContainerState
	taskInfo RTOSTask
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

func loadSandbox(ctx context.Context, id string) (sandbox *Sandbox, err error) {
	if id == "" {
		return nil, er.ErrEmptySandboxID
	}

	log.Debugf("trying to restore sandbox from disk")
	ss, err := RestoreSandbox(ctx, id)
	if err != nil {
		log.Debugf("failed to restore sandbox from disk: %v", err)
		return nil, err
	}
	c := ss.Config

	sandbox, err = createSandbox(ctx, &c)	
	if err != nil {
		log.Errorf("failed to create sandbox: %v", err)
		return nil, err
	}

	if err := sandbox.loadContainersToSandbox(ctx); err != nil {
		return nil, err
	}
	return sandbox, nil
}

// NOTICE: cleanup exclusively
func CleanupContainer(ctx context.Context, sandboxID string, containerID string, force bool) error {
	log.Debugf("cleaningup sandbox %s, container %s", sandboxID, containerID)
	if sandboxID == "" {
		return er.ErrEmptySandboxID
	}

	if containerID == "" {
		return er.ErrEmptyContainerID
	}

	// BUG: logic error, config loader is incomplete:
	//  1. multithread unsafe
	//  2. missing fields
	//  3. lacks verfications
	// sandbox, err := RestoreSandbox(ctx, sandboxID)
	sandbox, err := loadSandbox(ctx, sandboxID)
	if err != nil {
		return err
	}
	_, err = sandbox.StopContainer(ctx, containerID, force)
	if err != nil && !force {
		return err
	}

	_, err = sandbox.DeleteContainer(ctx, containerID)
	if err != nil && !force {
		return err
	}

	if len(sandbox.AllAnnotations()) > 0 {
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
// suppose that container config is already parsed!
func newContainer(ctx context.Context, s *Sandbox, cc *ContainerConfig) (*Container, error) {

	if cc == nil {
		return &Container{}, fmt.Errorf("container config is none")
	}

	if cc.ID == "" {
		log.Debugf("empty container id")
		return &Container{}, er.ErrEmptyContainerID
	}

	c := &Container{
		id:            cc.ID,
		sandbox:       s,
		sandboxId:     s.id,
		config:        cc,
		rootfs:        cc.Rootfs,
		containerPath: filepath.Join(s.id, cc.ID),
		mounts:        cc.Mount,
		// leave empty
		state:    ContainerState{},
		taskInfo: RTOSTask{},
		ctx:      s.ctx,
	}


	
	if err := c.RestoreState(); err != nil {
		log.Warnf("failed to restore container state: %v", err)
	}

	return c, nil
}

func (c *Container) start(ctx context.Context) error {

	if c.state.State == StateRunning {
		return fmt.Errorf("container %s is already running", c.id)
	}

	if c.state.State != StateReady && c.state.State != StateStopped {
		return fmt.Errorf("container is not ready or stopped, cannot start")
	}

	if err := c.state.ValidTransition(c.state.State, StateRunning); err != nil {
		return err
	}

	if err := startClient(ctx, c.sandbox, c); err != nil {
		log.Errorf("failed to start container: %v", err)
		if err := c.stop(ctx, true); err != nil {
			log.Warn("failed to stop the container after start failed")
		}
		return err
	}

	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) create(ctx context.Context) error {

	// TODO:  TOO many works
	rtosTask, err := createContainerInSandbox(c.sandbox, c.config)
	if err != nil {
		return err
	}

	c.taskInfo = *rtosTask

	if err := c.setContainerState(ctx, StateReady); err != nil {
		return err
	}
	return nil
}

func (c *Container) doStop(force bool) error {
	if c.state.State == StateStopped {
		log.Infof("Container %s is already stopped", c.id)
		return nil
	}

	if err := c.state.State.validTransition(c.state.State, StateStopped); err != nil && !force {
		return err
	}

	if err := libmica.Stop(c.ID()); err != nil {
		return err
	}
	return nil
}

func (c *Container) stop(ctx context.Context, force bool) error {

	var err error
	if err = c.doStop(force); err != nil {
		return err
	}

	if err = c.setContainerState(ctx, StateStopped); err != nil {
		return err
	}

	return nil
}

// consider kill as stop for now
// Due to the relationship that mica's Container:ClientOS:Task = 1:1:1, container kill() is basically contaienr stop()
func (c *Container) kill() error {
	if c.sandbox.state.State != StateReady && c.sandbox.state.State != StateRunning {
		return fmt.Errorf("sandbox is not running or ready, can not signal container")
	}
	log.Debugf("container state is %s", c.state.State)
	if c.state.State != StateRunning &&
		c.state.State != StateReady &&
		c.state.State != StatePaused {
		return fmt.Errorf("container is not running, ready or paused, can not be killed")
	}

	if err := c.doStop(true); err != nil {
		return err
	}

	c.state.State = StateStopped

	return nil
}

// Difference from mica: mica rm will force to stop client
// But for container engine, it is bad
func (c *Container) delete(ctx context.Context) error {
	if c.state.State != StateReady &&
		c.state.State != StatePaused &&
		c.state.State != StateStopped {
		return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete container")
	}

	if err := libmica.Remove(c.id); err != nil {
		log.Debugf("failed to remove container %s", err)
		return err
	}
	if err := c.sandbox.removeContainer(c.id); err != nil {
		return err
	}
	return c.sandbox.StoreSandbox(ctx)
}

func (c *Container) pause(ctx context.Context) error {
	if c.state.State != StateRunning {
		return fmt.Errorf("container is not running, cannot pause container")
	}
	err := libmica.Pause(c.id)
	if err != nil {
		return er.ErrMicadFailed
	}
	return c.setContainerState(ctx, StatePaused)
}

func (c *Container) resume(ctx context.Context) error {
	if c.state.State != StatePaused && c.sandbox.state.State != StateStopped {
		return fmt.Errorf("container is not paused, cannot resume container")
	}
	log.Infof("micran restart a client os, acting as `resume`")
	err := libmica.Start(c.id)
	if err != nil {
		return er.ErrMicadFailed
	}
	return c.setContainerState(ctx, StateRunning)
}

// TODO: container update resource
func (c *Container) update(ctx context.Context, resources specs.LinuxResources) error {
	log.Info("container dummy resource updated")
	return nil
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

// TODO: implement a POSIX signals hub
func (c *Container) Signal(ctx context.Context, signal syscall.Signal) error {
	if c.sandbox.notOperational() {
		return fmt.Errorf("sandbox is not running or ready, can not signal container")
	}
	if c.state.State != StateRunning && c.state.State != StateReady && c.state.State != StatePaused {
		return fmt.Errorf("client os is not running, ready or paused, can not signal container")
	}

	log.Errorf("container signal is not implemented")
	return errdefs.ErrNotImplemented
}

func validOS(os string) bool {
	ret := utils.InList(defs.PreservedOS[:], os)
	return ret
}

func validComponent(root,  component string) bool {
	file := filepath.Join(root, component)
	log.Debugf("file exist: %v", utils.FileExist(file))
	log.Debugf("file is regular: %v", utils.IsRegular(file))
	return utils.IsRegular(file)

}

func validFirmware(bundle, firmware string) bool {
	log.Debugf("validating firmware at %s", bundle)
	return validComponent(filepath.Join(bundle, "rootfs"), firmware)
}

// image.bin is For xen
// binpath: <bundle>/rootfs/<firmware>
func validBinfile(bundle, binpath string) bool {
	return validComponent(filepath.Join(bundle, "rootfs"), binpath)
}

func validCompatibility(_ *ContainerConfig) bool {
	// TODO: needed to ? how to check compatibility?
	return true
}

// NOTICE: Xen is the only supported ped for now
func (c *Container) validMicaContainer() bool {

	cwd, _ := os.Getwd()
	
	// Debug: Log the actual values being retrieved
	// log.Debugf("validMicaContainer - Configuration dump:")
	// log.Debugf("  GetOS(): %s", c.GetOS())
	// log.Debugf("  GetFirmwarePath(): '%s'", c.GetFirmwarePath())
	// log.Debugf("  GetPedestalConf(): '%s'", c.GetPedestalConf())
	// log.Debugf("  Container config.ElfPath: '%s'", c.config.ElfPath)
	// log.Debugf("  Container config.PedestalConf: '%s'", c.config.PedestalConf)
	// log.Debugf("  Current working directory: %s", cwd)
	
	osValid := validOS(c.GetOS())
	fwValid := validFirmware(cwd, c.GetFirmwarePath())
	if HostPedType == ped.Xen {
		binFile := validBinfile(cwd, c.GetPedestalConf())
		fwValid = binFile && fwValid
	}
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
	if c.cpuUnset() {
		if err := c.allocClientCPU(); err != nil {
			return c.config.cpu, err
		}
	}
	return c.config.cpu, nil
}

func (c *Container) SaveState() error {
	// Create serializable representation of container
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
		Config:        *c.config, // Dereference the pointer to save complete config
		TaskInfo:      c.taskInfo,
		Mounts:        c.mounts,
		ContainerPath: c.containerPath,
	}

	failed, failed1 := false, false
	var err error
	var err1 error

	stateInBundle := filepath.Join(c.containerPath, defs.MicantainerStateFile)
	stateInMicranDir := filepath.Join(defs.MicranStateDir, c.id, defs.MicantainerStateFile)

	// Ensure directories exist
	if err := utils.EnsureDir(filepath.Dir(stateInBundle), defs.DirMode); err != nil {
		log.Warnf("failed to ensure bundle directory: %v", err)
	}
	if err := utils.EnsureDir(filepath.Dir(stateInMicranDir), defs.DirMode); err != nil {
		log.Warnf("failed to ensure micran state directory: %v", err)
	}

	// Save to bundle directory
	if err = utils.SaveStructToJSON(stateInBundle, serializable); err != nil {
		failed = true
		err = fmt.Errorf("failed to save state to <%s>: %w", stateInBundle, err)
	}

	// Save to micran state directory
	if err1 = utils.SaveStructToJSON(stateInMicranDir, serializable); err1 != nil {
		failed1 = true
		err1 = fmt.Errorf("failed to save state to <%s>: %w", stateInMicranDir, err1)
	}

	if failed1 && failed {
		return fmt.Errorf("failed to save container state to both locations: %w, %w", err, err1)
	}
	return nil
}

func (c *Container) RestoreState() error {
	// Define the structure that matches what we store
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

	// Try to restore from micran state directory first
	stateInMicranDir := filepath.Join(defs.MicranStateDir, c.id, defs.MicantainerStateFile)
	raw, err := utils.RestoreStructFromJSON(stateInMicranDir)

	// If that fails, try bundle directory
	if err != nil {
		stateInBundle := filepath.Join(c.containerPath, defs.MicantainerStateFile)
		raw, err = utils.RestoreStructFromJSON(stateInBundle)
		if err != nil {
			return fmt.Errorf("failed to restore container state from both locations: %w", err)
		}
	}

	// Convert to JSON and then unmarshal into our struct
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("failed to marshal raw data: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, &storage); err != nil {
		return fmt.Errorf("failed to unmarshal container storage: %w", err)
	}

	// Restore the container state
	c.state = storage.State
	c.taskInfo = storage.TaskInfo
	c.mounts = storage.Mounts
	c.containerPath = storage.ContainerPath

	// Note: Config should already be set from sandbox restore
	// This method focuses on restoring runtime state

	return nil
}

// statisitcs returns the container statistics, only network contains now.
// TODO: extend stats range
func (c *Container) stats() (*ContainerStats, error) {

	if c.sandbox.state.State != StateRunning {
		return nil, fmt.Errorf("sandbox is not running, cannot stats container")
	}
	st := &ContainerStats{}
	return st, nil
}

// TODO: for now, taskId is always a dummy because one client, one task
// BUT It is possible to apply a new task to the client os in future,
// TALK: By Xen?
func (c *Container) wait4exit() (int32, error) {
	if c.notOperational() {
		return int32(er.UnexpectedStatus), errors.New("container is not ready or running, cannot wait for exit")
	}
	err := libmica.Stop(c.ID())
	if err != nil {
		return int32(er.MicadFailed), err
	}
	return ok0, nil
}

func (c *Container) ioStream(taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	if c.notOperational() {
		return nil, nil, nil, fmt.Errorf("Container not ready or running, impossible to signal the container")
	}

	stream := newIOStream(c.sandbox, c, taskID)

	return stream.stdin(), stream.stdout(), stream.stderr(), nil
}

// TODO: resize the terminal connected to /dev/ttyRPMSG*
func (c *Container) winresize(height, width uint32) error {
	if c.notOperational() {
		return fmt.Errorf("Container not ready or running, impossible to resize the container pty")
	}
	log.Debugf("resize pty -> %dx%d", width, height)
	return errdefs.ErrNotImplemented
}

func (c *Container) GetFirmwarePath() string {
	return c.config.ElfPath
}

func (c *Container) GetPedestalConf() string {
	return c.config.PedestalConf
}

func (c *Container) GetOS() string {
	return c.config.OS
}

func (c *Container) cpuUnset() bool {
	return c.config.cpu == -1
}

func (c *Container) GetPedGuestBootBin() string {
	if HostPedType == ped.Xen {
		return c.config.PedestalConf
	}
	return ""
}

// PedestalType returns the pedestal type
func (c *Container) GetPedestalType() ped.PedType {
	return c.config.PedestalType
}

// container is not ready for being operated
func (c *Container) notOperational() bool {
	return c.state.State != StateReady && c.state.State != StateRunning
}

package micantainer

import (
	"context"
	"fmt"
	"io"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	er "mica-shim/pkg/errors"
	"mica-shim/pkg/fileutils"
	"mica-shim/pkg/libmica"
	ped "mica-shim/pkg/pedestal"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/containerd/errdefs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

// Status is a graph of the sanbox, contains more than state
type SandboxStatus struct {
	ContainersState []ContainerStatus
	Annotations     map[string]string
	ID              string
	State           SandboxState
}

type SandboxStats struct {
	Cpus int
}

type SandboxConfig struct {
	ID               string
	Hostname         string
	NetworkConfig    NetworkConfig
	PedConfig        ped.PedConfig
	ContainerConfigs map[string]*ContainerConfig
	Annotations      map[string]string
	// TODO: Pod resource
	// Maybe crutial for sandbox, we just set shared memory size here
	// The actual memory management is not micran's work, but micad's
	// ShmSize uint64
	SharedMemorySize uint64
	SandboxResources SandboxResourceSizing
}

type SandboxResourceSizing struct {
	// The number of CPUs required for the sandbox workload(s)
	WorkloadCPUs uint32
	// The base number of CPUs for the VM that are assigned as overhead
	BaseCPUs uint32
	// The amount of memory required for the sandbox workload(s)
	WorkloadMemMB uint32
	// The base amount of memory required for that RTOS Client that is assigned as overhead
	BaseMemMB uint32
}

type StateString string

const (
	StateReady    StateString = "ready"
	StateRunning  StateString = "running"
	StateStopped  StateString = "stopped"
	StateCreating StateString = "creating"
	// Unsupported yet
	StatePaused StateString = "paused"
)

type SandboxState struct {
	State StateString
	// Unified configurations of client rtos managed by Sandbox
	Ped     string
	Version uint
}

// SandboxState public methods

func (s *SandboxState) Valid() bool {
	return s.State.valid()
}

func (s *SandboxState) Transition(old StateString, new StateString) error {
	if s.Valid() {
		return fmt.Errorf("invalid state: %v", s)
	}

	return s.State.validTransition(old, new)
}

// Private methods

func (s *StateString) valid() bool {
	for _, validState := range []StateString{StateReady, StateRunning, StateStopped, StateCreating, StatePaused} {
		if *s == validState {
			return true
		}
	}
	return false
}

func (s *StateString) validTransition(old StateString, new StateString) error {
	if *s != old {
		return fmt.Errorf("invalid state: %v (expected: %v)", s, old)
	}

	switch *s {
	case StateReady:
		if new == StateRunning || new == StateStopped {
			return nil
		}

	case StateRunning:
		if new == StatePaused || new == StateStopped {
			return nil
		}

	case StatePaused:
		if new == StateRunning || new == StateStopped {
			return nil
		}

	case StateStopped:
		if new == StateRunning {
			return nil
		}
	}

	return fmt.Errorf("cannot transition from state %v to %v", s, new)
}

// expand fields of sandboxconfigs as sandbox memebers
type Sandbox struct {
	ctx context.Context
	mu  sync.Mutex
	// fs, storage, devices, volumes...
	// monitor
	agent      RealAgent
	config     SandboxConfig
	containers map[string]*Container
	id         string
	network    Network
	state      SandboxState

	annotaLock sync.RWMutex
	wg         sync.WaitGroup
}

// impl SandboxTraits for Sandbox
func (s *Sandbox) GetAllContainers() []ContainerTraits {
	list := make([]ContainerTraits, len(s.containers))
	for _, c := range s.containers {
		list = append(list, c)
	}
	return list
}

func (s *Sandbox) SandboxID() string {
	return s.id
}

func (s *Sandbox) Annotation(key string) (string, error) {
	s.annotaLock.RLock()
	defer s.annotaLock.RUnlock()
	value, found := s.config.Annotations[key]
	if !found {
		return "", fmt.Errorf("annotation not found: %s", key)
	}
	return value, nil
}

func (s *Sandbox) SetAnnotations(annotations map[string]string) {
	s.annotaLock.Lock()
	defer s.annotaLock.Unlock()
	for k, v := range annotations {
		s.config.Annotations[k] = v
	}

}

func (s *Sandbox) AllAnnotations() map[string]string {
	s.annotaLock.RLock()
	defer s.annotaLock.RUnlock()
	return s.config.Annotations
}

func (s *Sandbox) CheckDaemon() *libmica.MicaDaemonState {
	state, err := libmica.DaemonState()
	if err != nil {
		log.Warnf("failed to fetch daemon state: %v", err)
		return nil
	}
	log.Pretty("%v", state)
	return state
}

func (s *Sandbox) Monitor() {

}

func (s *Sandbox) GetNetNamespace() string {
	return s.network.NetID()
}

func (s *Sandbox) GetContainer(id string) ContainerTraits {
	return s.containers[id]
}

// status of containers and sandbox itself;
func (s *Sandbox) Start(ctx context.Context) error {
	if err := s.state.Transition(StateReady, StateRunning); err != nil {
		return err
	}

	oldState := s.state.State
	if err := s.setSandboxState(StateRunning); err != nil {
		return err
	}

	var startErr error
	defer func() {
		if startErr != nil {
			s.setSandboxState(oldState)
		}
	}()
	for _, c := range s.containers {
		if startErr = c.start(ctx); startErr != nil {
			return startErr
		}
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	log.Infof("sandbox %s started", s.id)
	return nil
}

// Stop stops all containers inside the sandbox as well as sandbox itself
func (s *Sandbox) Stop(ctx context.Context, force bool) error {
	if s.state.State == StateStopped {
		log.Infof("sandbox %s is already stopped", s.id)
		return nil
	}

	if err := s.state.Transition(s.state.State, StateStopped); err != nil {
		return err
	}

	for _, c := range s.containers {
		if err := c.stop(ctx, force); err != nil {
			return err
		}
	}

	// TODO: add stopClient()
	if err := s.stopClient(ctx); err != nil && !force {
		return err
	}

	// TODO: IO and monitor stopped
	log.Debug("stop monitor and console")

	if err := s.setSandboxState(StateStopped); err != nil {
		return err
	}

	if err := s.removeNetwork(ctx); err != nil && !force {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

// Stop rtos clients && sandbox
func (s *Sandbox) Delete(ctx context.Context) error {
	if s.state.State != StateReady &&
		s.state.State != StatePaused &&
		s.state.State != StateStopped {
		return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete")
	}

	for _, c := range s.containers {
		if err := c.delete(ctx); err != nil {
			log.Errorf("failed to cleanup container %s", c.id)
		}
	}

	s.agent.Cleanup(ctx)

	return s.cleanSandboxStorage()
}

// CreateContainer creates a new container in the sandbox
// This should be called only when the sandbox is already created.
// It will add new container config to sandbox.config.Containers
func (s *Sandbox) CreateContainer(ctx context.Context, config ContainerConfig) (ContainerTraits, error) {
	id := config.ID
	if _, ok := s.containers[id]; ok {
		log.Errorf("container %s already exists", id)
		return nil, er.ErrAlreadyExists
	}
	s.config.ContainerConfigs[id] = &config
	newc := s.config.ContainerConfigs[id]

	var err error
	defer func() {
		if err != nil {
			if len(s.config.ContainerConfigs) > 0 {
				delete(s.config.ContainerConfigs, id)
			}
		}
	}()

	c, err := newContainer(ctx, s, newc)
	if err = c.create(ctx); err != nil {
		return nil, err
	}

	if err = s.addContainer(c); err != nil {
		return nil, err

	}
	defer func() {
		if err != nil {
			log.Errorf("failed to create container %s: %v", id, err)
		}

		if errStop := c.stop(ctx, true); errStop != nil {
			log.Errorf("failed to stop container %s after creation failure: %v", id, errStop)
		}
		log.Debug("remove stopped container from sandbox")
		s.removeContainer(c.id)
	}()

	if err = s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}

	// update sandbox status
	if err = s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) Status() SandboxStatus {
	var cStatusList []ContainerStatus
	for _, c := range s.containers {
		rootfs := c.config.Rootfs.Source
		if c.config.Rootfs.Mounted {
			rootfs = c.config.Rootfs.Target
		}
		s := ContainerStatus{
			ID:          c.id,
			State:       c.state,
			Rootfs:      rootfs,
			Annotations: c.config.Annotations,
			StartedAt:   c.taskInfo.StartTime,
			Pid:         c.config.Pid,
		}
		cStatusList = append(cStatusList, s)
	}

	return SandboxStatus{
		ContainersState: cStatusList,
		Annotations:     s.config.Annotations,
		ID:              s.id,
		State:           s.state,
	}
}

func (s *Sandbox) removeContainer(containerID string) error {
	if s == nil {
		return fmt.Errorf("sandbox is nil")
	}

	if containerID == "" {
		return er.ErrEmptyContainerID
	}

	if _, ok := s.containers[containerID]; !ok {
		return errors.Wrapf(er.ErrContainerNotFound, "Could not remove the container %q from the sandbox %q containers list",
			containerID, s.id)
	}

	delete(s.containers, containerID)
	delete(s.config.ContainerConfigs, containerID)
	return nil
}

func (s *Sandbox) DeleteContainer(ctx context.Context, id string) (ContainerTraits, error) {
	if s == nil {
		return nil, er.ErrSandboxNil
	}
	if id == "" {
		return nil, er.ErrEmptyContainerID
	}

	c, ok := s.containers[id]
	if !ok {
		return nil, er.ErrContainerNotFound
	}
	if err := c.delete(ctx); err != nil {
		return nil, err
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StartContainer(ctx context.Context, id string) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ErrContainerNotFound
	}

	// start client os, os start the task from entry inside the OS image
	if err := c.start(ctx); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StopContainer(ctx context.Context, id string, force bool) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ErrContainerNotFound
	}
	if err := c.stop(ctx, force); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Stop the container forcely and pop it, not storing sandbox state
func (s *Sandbox) KillContainer(ctx context.Context, id string) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ErrContainerNotFound
	}
	if err := c.kill(); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StatusContainer(id string) (ContainerStatus, error) {
	cs := ContainerStatus{}
	if id == "" {
		return cs, er.ErrEmptyContainerID
	}

	if c, ok := s.containers[id]; ok {
		rootfs := c.config.Rootfs.Source
		if c.config.Rootfs.Mounted {
			rootfs = c.config.Rootfs.Target
		}

		// TODO: no need to store starttime in taskinfo, collapsing is unneeded
		cs.Spec = nil
		cs.StartedAt = c.taskInfo.StartTime
		cs.State = c.state
		cs.ID = c.id
		cs.Rootfs = rootfs
		cs.Pid = c.GetPid()
		cs.Annotations = c.config.Annotations
		return cs, er.ErrContainerNotFound
	}
	log.Debugf("container %s not found in sandbox %s", id, s.id)
	return cs, nil
}

func (s *Sandbox) StatsContainer(ctx context.Context, id string) (ContainerStats, error) {
	c, ok := s.containers[id]
	if !ok {
		return ContainerStats{}, er.ErrContainerNotFound
	}

	stats, err := c.stats(ctx)
	if err != nil {
		log.Errorf("failed to get stats for container %s: %v", id, err)
		return ContainerStats{}, err
	}
	return *stats, nil
}

func (s *Sandbox) Stats(ctx context.Context) (SandboxStats, error) {
	stats := SandboxStats{}

	// BUG: logic leaks
	vCpuNum, err := s.agent.vcpuSet(ctx)
	if err != nil {
		log.Errorf("failed to get vcpu number: %v", err)
		return stats, err
	}
	stats.Cpus = int(vCpuNum)
	return stats, nil
}

func (s *Sandbox) IOStream(containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	if s.state.State != StateRunning {
		return nil, nil, nil, er.ErrSandboxDown
	}

	c, ok := s.containers[containerID]
	if !ok {
		return nil, nil, nil, er.ErrContainerNotFound
	}

	return c.ioStream(taskID)
}

func (s *Sandbox) GetOOMEvent(ctx context.Context) (string, error) {
	return "", nil
}

// Not supported well
// TODO: aftet unified micran and micad, we can achive sending signals to RTOS clients
// NOTICE: container == task == RTOS Client
func (s *Sandbox) WaitTaskExit(ctx context.Context, containerID string, taskid string) (int32, error) {
	if s.state.State != StateRunning {
		return 0, er.ErrSandboxDown
	}
	c, ok := s.containers[containerID]
	if !ok {
		return 0, er.ErrContainerNotFound
	}

	return c.wait4exit(ctx, taskid)
}

func (s *Sandbox) SignalTask(ctx context.Context, containerID string, signal syscall.Signal, all bool) error {
	// if all, ignore the containerID parameter
	if all {
		log.Debugf("boardcast signal %s for all containers in sandbox %s", uint32(signal), s.id)
		for _, c := range s.containers {
			if err := c.Signal(ctx, signal); errors.Is(err, er.ErrInvalidSig) || err == nil {
				continue
			} else {
				log.Errorf("failed to signal container %s: %v", c.ID(), err)
			}
		}
	} else {
		log.Debugf("sending signal %s for containers %s in sandbox %s", uint32(signal), containerID, s.id)
		c, ok := s.containers[containerID]
		if !ok || c == nil {
			return er.ErrContainerNotFound
		}

		return c.Signal(ctx, signal)
	}
	return nil
}

func (s *Sandbox) WinsizeTask(ctx context.Context, containerID, processID string, height, width uint32) error {
	return errdefs.ErrNotImplemented
}

func (s *Sandbox) PauseContainer(ctx context.Context, id string) error {

	c, ok := s.containers[id]
	if !ok {
		return er.ErrContainerNotFound
	}

	if err := c.pause(ctx); err != nil {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Sandbox) ResumeContainer(ctx context.Context, id string) error {
	c, ok := s.containers[id]
	if !ok {
		return er.ErrContainerNotFound
	}

	if err := c.resume(ctx); err != nil {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Sandbox) UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error {
	log.Debugf("Updated container %v resources", resources)
	return nil
}

// privates:

func (s *Sandbox) setSandboxState(state StateString) error {
	if state == "" {
		return er.ErrInvalidState
	}
	s.state.State = state
	return nil
}

type FlattenedSandboxState struct {
	// sandboxContainer SandboxContainerID
	SandboxContainerID string        `json:"sandbox_container_id"`
	State              StateString   `json:"state"`
	Network            NetworkConfig `json:"network"`
	Version            uint          `json:"version"`
}

func (s *Sandbox) dumpState(f *FlattenedSandboxState) {
	f.SandboxContainerID = s.id
	f.State = s.state.State
}

func (s *Sandbox) dumpNet(f *FlattenedSandboxState) {
	if dummyNetwork, ok := s.network.(*DummyNetwork); ok {
		id := dummyNetwork.NetID()
		created := dummyNetwork.NetworkIsCreated()
		netConfig := NetworkConfig{
			NetworkID:      id,
			NetworkCreated: created,
		}
		f.Network = netConfig
	}
}

func (s *Sandbox) dumpVersion(f *FlattenedSandboxState) {
	v := s.state.Version
	if v == 0 {
		v = defs.SandboxVersion
	}
	f.Version = v
}

func (s *Sandbox) flat() FlattenedSandboxState {

	flag := FlattenedSandboxState{}
	s.dumpState(&flag)
	s.dumpNet(&flag)
	s.dumpVersion(&flag)

	return flag
}

func (s *Sandbox) StoreSandbox(ctx context.Context) error {
	flatterned := s.flat()
	target, err := s.newSandboxStoragePath()
	if err != nil {
		return err
	}
	if err := fileutils.SaveStructToJSON(target, flatterned); err != nil {
		return err
	}
	return nil
}

func (s *Sandbox) sandboxStoragePath() string {
	return filepath.Join(defs.SandboxDataDir, s.id)
}

func (s *Sandbox) newSandboxStoragePath() (string, error) {
	dir := s.sandboxStoragePath()
	if err := os.MkdirAll(dir, defs.DirMode); err != nil {
		return "", err
	}
	stateFile := filepath.Join(dir, defs.SandboxStateFile)
	return stateFile, nil
}

func (s *Sandbox) addContainer(c *Container) error {

	if _, ok := s.containers[c.id]; ok {
		return er.ErrDuplicatedKey
	}
	s.containers[c.id] = c
	return nil
}

func (s *Sandbox) cleanSandboxStorage() error {
	if s.id == "" {
		return er.ErrEmptySandboxID
	}
	dir := s.sandboxStoragePath()
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return nil

}
func (s *Sandbox) removeNetwork(ctx context.Context) error {
	log.Infof("removed network of sandbox %s", s.id)
	log.Debugf("remove network for sandbox %s", s.id)
	return nil
}

func (s *Sandbox) stopClient(ctx context.Context) error {
	log.Debugf("stop sandbox %s", s.id)
	if err := s.agent.stopSandbox(ctx, s); err != nil {
		log.Errorf("failed to stop sandbox %s: %v", s.id, err)
		return err
	}
	log.Info("stopping client os")
	return nil
}

// considering pinning vCPUs on different pedestal
func (s *Sandbox) checkVCPUsPinning(ctx context.Context) error {
	return nil
}

// DummySandboxConfig creates a minimal sandbox config for quick development
func DummySandboxConfig(cid string, spec *specs.Spec) (*SandboxConfig, error) {
	return &SandboxConfig{
		ID:       cid,
		Hostname: spec.Hostname,
		Annotations: map[string]string{
			"org.openeuler.mica.test": "true",
		},
		ContainerConfigs: make(map[string]*ContainerConfig),
		SharedMemorySize: 64 * 1024 * 1024, // 64MB
		SandboxResources: SandboxResourceSizing{
			WorkloadCPUs:  1,
			BaseCPUs:      1,
			WorkloadMemMB: 128,
			BaseMemMB:     64,
		},
	}, nil
}

// setup sandbox
func CreateSandbox(ctx context.Context, cfg *SandboxConfig) (*Sandbox, error) {
	s, err := newSandbox(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if s.state.State != "" {
		return s, nil
	}

	if err = s.setSandboxState(StateReady); err != nil {
		return nil, err
	}

	return s, nil
}

// newSandbox creates a new sandbox instance
func newSandbox(ctx context.Context, config *SandboxConfig) (*Sandbox, error) {
	s := &Sandbox{
		ctx:        ctx,
		config:     *config,
		containers: make(map[string]*Container),
		id:         config.ID,
		state: SandboxState{
			State:   StateCreating,
			Ped:     HostPedType.String(),
			Version: defs.SandboxVersion,
		},
		network: &DummyNetwork{},
		agent:   *NewMockRealRealAgent(),
	}

	// Initialize the sandbox
	if _, err := s.agent.init(ctx, s); err != nil {
		return nil, err
	}

	// Create the sandbox through the agent
	if err := s.agent.createSandbox(ctx, s); err != nil {
		return nil, err
	}

	// Set initial state
	if err := s.setSandboxState(StateReady); err != nil {
		return nil, err
	}

	return s, nil
}

// LoadSandboxFromStorage loads an existing sandbox from storage, by sandbox id
// TODO finish LoadSandboxFromStorage
func LoadSandboxFromStorage(ctx context.Context, id string) (*Sandbox, error) {
	// Load sandbox configuration from storage
	configPath := filepath.Join(defs.SandboxDataDir, id, defs.SandboxStateFile)
	flattened := FlattenedSandboxState{}
	raw, err := fileutils.RestoreStructFromJSON(configPath)
	if err != nil {
		return nil, err
	}

	// Type assert and convert the raw data to FlattenedSandboxState
	rawMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("failed to parse sandbox state file")
	}

	// Extract fields from the map
	if sandboxID, ok := rawMap["sandbox_container_id"].(string); ok {
		flattened.SandboxContainerID = sandboxID
	}

	if state, ok := rawMap["state"].(string); ok {
		flattened.State = StateString(state)
	}

	if version, ok := rawMap["version"].(float64); ok {
		flattened.Version = uint(version)
	}

	// Extract network config
	if network, ok := rawMap["network"].(map[string]interface{}); ok {
		if networkID, ok := network["network_id"].(string); ok {
			flattened.Network.NetworkID = networkID
		}
		if networkCreated, ok := network["network_created"].(bool); ok {
			flattened.Network.NetworkCreated = networkCreated
		}
	}

	// Create a new sandbox with the loaded configuration
	network := &DummyNetwork{}

	s := &Sandbox{
		ctx:        ctx,
		containers: make(map[string]*Container),
		id:         id,
		state: SandboxState{
			State:   flattened.State,
			Ped:     HostPedType.String(),
			Version: flattened.Version,
		},
		network: network,
		agent:   *NewMockRealRealAgent(),
	}

	return s, nil
}

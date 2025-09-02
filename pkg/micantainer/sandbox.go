package micantainer

import (
	"context"
	"encoding/json"
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

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

const (
	ok0 = 0
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
	ID                string
	Hostname          string
	NetworkConfig     NetworkConfig
	PedType           ped.PedType
	PedConfig         ped.PedConfig
	ContainerConfigs  map[string]*ContainerConfig
	Annotations       map[string]string
	SharedMemorySize  uint64
	SandboxResources  SandboxResourceSizing
	EnableVCPUsPining bool
	// TALK: consider static management
}

func (sc *SandboxConfig) valid() bool {
	if sc.ID == "" {
		log.Warn("sandbox ID is empty")
		return false
	}

	if sc.PedType == ped.Unsupported {
		log.Warn("pedestal type is unsupported")
		return false
	}

	return true
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
	State   StateString
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
	// use annoymous field to avoid unused fields wanring
	sync.Mutex
	// fs, storage, devices, volumes...
	// monitor
	agent      RealAgent
	config     SandboxConfig
	containers map[string]*Container
	id         string
	network    Network
	state      SandboxState

	annotaLock *sync.RWMutex
	wg         *sync.WaitGroup
}

// impl SandboxTraits for Sandbox
func (s *Sandbox) GetAllContainers() []ContainerTraits {
	list := make([]ContainerTraits, 0, len(s.containers))
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

func (s *Sandbox) DaemonState() *libmica.MicaDaemonState {
	state, err := libmica.DaemonState()
	if err != nil && !errors.Is(err, er.ErrMicadNotRunning) {
		log.Warnf("failed to fetch daemon state: %v", err)
		return nil
	}
	log.Pretty("%v", state)
	return state
}

func (s *Sandbox) Monitor() {
	return
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
	log.Debugf("remove container %s", containerID)
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
	log.Debugf("delete container %s from sandbox", id)
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
		log.Debugf("status container: empty id")
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

	stats, err := c.stats()
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
		return ok0, er.ErrSandboxDown
	}
	c, ok := s.containers[containerID]
	if !ok {
		return int32(er.NotFound), er.ErrContainerNotFound
	}

	return c.wait4exit()
}

func (s *Sandbox) SignalTask(ctx context.Context, containerID string, signal syscall.Signal) error {
	if s.state.State != StateRunning {
		return er.ErrSandboxDown
	}

	log.Debugf("sending signal %s for containers %s in sandbox %s", uint32(signal), containerID, s.id)
	c, ok := s.containers[containerID]
	if !ok || c == nil {
		return er.ErrContainerNotFound
	}

	return c.Signal(ctx, signal)
}

func (s *Sandbox) WinResize(ctx context.Context, containerID string, height, width uint32) error {
	if s.state.State != StateRunning {
		return er.ErrSandboxDown
	}

	c, ok := s.containers[containerID]
	if c == nil || !ok {
		return er.ErrContainerNotFound
	}

	return c.winresize(height, width)
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

// store sandbox information to disk
func (s *Sandbox) StoreSandbox(ctx context.Context) error {
	target, err := s.newSandboxStoragePath()
	log.Debugf("store sandbx ==> %s", target)
	if err != nil {
		return err
	}

	// Create serializable representation of sandbox
	serializable := SandboxStorage{
		ID:     s.id,
		State:  s.state,
		Config: s.config,
	}

	// Get network config if needed
	if dummyNetwork, ok := s.network.(*DummyNetwork); ok {
		serializable.Network = NetworkConfig{
			NetworkID:      dummyNetwork.NetID(),
			NetworkCreated: dummyNetwork.NetworkIsCreated(),
		}
	}

	if err := fileutils.SaveStructToJSON(target, serializable); err != nil {
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
	s, err := createSandboxFromConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func newSandbox(ctx context.Context, config SandboxConfig) (sb *Sandbox, retErr error) {
	if !config.valid() {
		return nil, fmt.Errorf("invalid sandbox configuration")
	}
	network := DummyNetwork{}
	s := &Sandbox{
		ctx:        ctx,
		config:     config,
		containers: make(map[string]*Container),
		id:         config.ID,
		state: SandboxState{
			State:   StateCreating,
			Ped:     HostPedType.String(),
			Version: defs.SandboxVersion,
		},
		network:    &network,
		agent:      *NewAgent(),
		wg:         &sync.WaitGroup{},
		annotaLock: &sync.RWMutex{},
	}

	if err := s.Restore(); err != nil {
		log.Debugf("failed to restore sandbox %s: %v", s.id, err)
	}
	return s, nil
}


func createSandbox(ctx context.Context, config *SandboxConfig) (*Sandbox, error) {

	s, err := newSandbox(ctx, *config)
	if err != nil {
		return nil, err
	}

	if s.state.State == StateReady || s.state.State == StateRunning {
		log.Debugf("sandbox already in ready/running state, creation finished.")
		return s, nil
	}

	hostname := s.config.Hostname
	if len(hostname) > maxHostnameLength {
		hostname = hostname[:maxHostnameLength]
	}
	s.config.Hostname = hostname

	if err := s.setSandboxState(StateReady); err != nil {
		return nil, err
	}

	return s, nil
}

// createSandboxFromConfig creates a new sandbox instance from sandbox config
// 1. createSandboxFromConfig instance, and setup
// 2. cleanup if error happens
// 3.
func createSandboxFromConfig(ctx context.Context, config *SandboxConfig) (*Sandbox, error) {
	s, err := createSandbox(ctx, config)

	defer func() {
		if err != nil {
			log.Debugf("hooked delete sandbox!")
			s.Delete(ctx)
		}
	}()

	if err = s.createNetwork(ctx); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			s.removeNetwork(ctx)
		}
	}()

	s.postNetworkCreated()

	if err = s.initContainers(ctx); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Sandbox) Restore() error {
	ss, err := RestoreSandbox(s.ctx, s.id)

	if err != nil {
		log.Warnf("failed to restore sandbox state: %v", err)
		return nil
	}

	if ss != nil {
		if ss.ID != s.id {
			log.Debugf("sandbox ID mismatch: %v != %v", ss.ID, s.id)
			log.Pretty("%v", ss)
			return fmt.Errorf("sandbox ID mismatch: %v != %v", ss.ID, s.id)
		}

		s.state.Ped = ss.State.Ped
		s.state.Version = ss.State.Version
		s.state.State = ss.State.State
		s.config = ss.Config
		s.network = &ss.Network
	}

	return nil
}

// Define the structure that matches what we store
type SandboxStorage struct {
	ID      string        `json:"id"`
	State   SandboxState  `json:"state"`
	Config  SandboxConfig `json:"config"`
	Network NetworkConfig `json:"network"`
	// Containers map[string]*Container `json:"containers"`
}

// RestoreSandbox loads an existing sandbox from storage, by sandbox id
func RestoreSandbox(ctx context.Context, id string) (*SandboxStorage, error) {
	// Load sandbox configuration from storage
	sandboxDir := filepath.Join(defs.SandboxDataDir, id)
	configPath := filepath.Join(sandboxDir, defs.SandboxStateFile)

	raw, err := fileutils.RestoreStructFromJSON(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("not found sandbox state file: %s, a new one should be created", configPath)
			return nil, err
		}
		return nil, fmt.Errorf("failed to load sandbox state from %s: %w", configPath, err)
	}

	// Convert to JSON and then unmarshal into our struct
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal raw data: %w", err)
	}

	var storage SandboxStorage
	if err := json.Unmarshal(jsonBytes, &storage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sandbox storage: %w", err)
	}

	// Return just the state part as requested by the function signature
	return &storage, nil
}

// sandbox is not ready for being operated
func (s *Sandbox) notOperational() bool {
	return s.state.State != StateReady && s.state.State != StateRunning
}

func (s *Sandbox) createNetwork(ctx context.Context) error {
	log.Debugf("createNetwork.")
	return nil
}

func (s *Sandbox) postNetworkCreated() error {
	if netConfig, ok := s.network.(*NetworkConfig); ok {
		return netConfig.postCreated()
	}
	return nil
}

// add containers to sandbox
func (s *Sandbox) initContainers(ctx context.Context) error {
	log.Pretty("initContainers: %v", s.config.ContainerConfigs)
	for _, cc := range s.config.ContainerConfigs {
		c, err := newContainer(ctx, s, cc)
		if err != nil {
			return err
		}
		if err := c.create(ctx); err != nil {
			return err
		}
		if err := s.addContainer(c); err != nil {
			return err
		}
	}

	if err := s.updateResources(ctx); err != nil {
		return err
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

// TODO: considering pinning vCPUs on different pedestal
func (s *Sandbox) checkVCPUsPinning(ctx context.Context) error {
	return nil
}

func (s *Sandbox) updateResources(ctx context.Context) error {
	return nil
}

// creates new container instances in sandbox
func (s *Sandbox) loadContainersToSandbox(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("loadContainersToSandbox: is only called an existing sandbox")
	}

	for _, cc := range s.config.ContainerConfigs {
		c, err := newContainer(ctx, s, cc)
		if err != nil {
			return err
		}

		if err := s.addContainer(c); err != nil {
			return err
		}
	}

	return nil

}
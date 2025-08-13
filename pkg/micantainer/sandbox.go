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
	"os"
	"path/filepath"
	"sync"
	"syscall"

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

type SandboxConfig struct {
	ID               string
	Hostname         string
	NetworkConfig    NetworkConfig
	PedConfig        PedConfig
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
	agent      Agent
	config     SandboxConfig
	containers map[string]*Container
	id         string
	network    Network
	state      SandboxState

	annotaLock sync.RWMutex
	wg         sync.WaitGroup
}

// impl SandboxTraits for Sandbox
func (s *Sandbox) GetAllContainers() []ContainerTrait {
	list := make([]ContainerTrait, len(s.containers))
	for _, c := range s.containers {
		list = append(list, c)
	}
	return list
}

func (s *Sandbox) GetContainer(id string) ContainerTrait {
	return s.containers[id]
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

func (s *Sandbox) AllAnnotations() map[string]string {
	s.annotaLock.RLock()
	defer s.annotaLock.RUnlock()
	return s.config.Annotations
}

func (s *Sandbox) GetNetNamespace() string {
	return s.network.NetID()
}

func (s *Sandbox) SetAnnotations(annotations map[string]string) {
	s.annotaLock.Lock()
	defer s.annotaLock.Unlock()
	for k, v := range annotations {
		s.config.Annotations[k] = v
	}
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

// status of containers and sandbox itself;
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

	return s.rmSandboxStorage()
}

func (s *Sandbox) CreateContainer(ctx context.Context, config ContainerConfig) (ContainerTrait, error) {

	id := config.ID
	if _, ok := s.containers[id]; ok {
		log.Warnf("container %s already exists", id)
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
	if err != nil {
		return nil, err
	}
	if err = c.createInSanbox(ctx); err != nil {
		return nil, err
	}

	s.containers[id] = c

	defer func() {
		if err != nil {
			log.Errorf("cleaning up created container %s", id)
			if errStop := c.stop(ctx, true); errStop != nil {
				log.Error("failed to stop the newly-created container inside box")
			}
			s.removeContainer(c.id)
		}
	}()
	if err = s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
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

func (s *Sandbox) DeleteContainer(ctx context.Context, id string) (ContainerTrait, error) {
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

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StartContainer(ctx context.Context, id string) (ContainerTrait, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ErrContainerNotFound
	}
	if err := c.start(ctx); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StopContainer(ctx context.Context, id string, force bool) (ContainerTrait, error) {
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

func (s *Sandbox) KillContainer(ctx context.Context, id string) (ContainerTrait, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ErrContainerNotFound
	}
	if err := c.kill(ctx, true); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StatusContainer(id string) (ContainerState, error) {
	cs := ContainerState{}
	if id == "" {
		return cs, er.ErrEmptyContainerID
	}

	// TODO:  status contaienr of sandbox
	if _, ok := s.containers[id]; !ok {
		log.Debugf("container %s not found in sandbox %s", id, s.id)
		return cs, er.ErrContainerNotFound
	}
	return cs, nil
}

func (s *Sandbox) StatsContainer(ctx context.Context, id string) (ContainerStats, error) {
	return ContainerStats{}, nil
}

func (s *Sandbox) WaitContainer(ctx context.Context, id string, pid string) (int32, error) {
	return 0, nil
}

func (s *Sandbox) IOStream(containerID, processID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	return nil, nil, nil, nil
}

func (s *Sandbox) GetOOMEvent(ctx context.Context) (string, error) {
	return "", nil
}

// Not supported well
// TODO: aftet unified micran and micad, we can achive sending signals to RTOS clients
func (s *Sandbox) WaitTaskExit(ctx context.Context, containerID string, taskid uint32) (int32, error) {
	return libmica.ConvertToPosixError("zephyr", 0), nil
}

func (s *Sandbox) SignalTask(ctx context.Context, containerID, processID string, signal syscall.Signal, all bool) error {
	return nil
}

func (s *Sandbox) WinsizeTask(ctx context.Context, containerID, processID string, height, width uint32) error {
	return nil
}

func (s *Sandbox) PauseContainer(ctx context.Context, id string) error {
	return nil
}

func (s *Sandbox) ResumeContainer(ctx context.Context, id string) error {
	return nil
}

func (s *Sandbox) UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error {
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

// extracted from sandbox, for serialization
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
	id := s.network.NetID()
	created := s.network.NetworkIsCreated()
	netConfig := NetworkConfig{
		NetworkID:      id,
		NetworkCreated: created,
	}
	f.Network = netConfig
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
	if err := fileutils.SaveStructToFile(target, flatterned); err != nil {
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

func (s *Sandbox) rmSandboxStorage() error {
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

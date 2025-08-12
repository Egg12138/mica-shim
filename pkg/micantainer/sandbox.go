package micantainer

import "fmt"

// Status is a graph of the sanbox, contains more than state
type SandboxStatus struct {
	ContainersState []ContainerStatus
	Annotations     map[string]string
	ID              string
	State           SandboxState
}

type SandboxConfig struct {
	ID            string
	Hostname      string
	NetworkConfig NetworkConfig
	PedConfig     PedConfig
	//
	// TODO: Pod resource
	// Maybe crutial for sandbox, we just set shared memory size here
	// The actual memory management is not micran's work, but micad's
	// ShmSize uint64
	SharedMemorySize uint64
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
	Ped string
}

// SandboxState public methods

func (s *SandboxState) Valid() bool {
	return s.State.valid()
}

func (s *SandboxState) ValidTransition(old StateString, new StateString) error {
	if s.Valid() {
		return fmt.Errorf("Invalid state %v", s)
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
		return fmt.Errorf("Invalid state %v (Expecting %v)", s, old)
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

	return fmt.Errorf("Can not move from %v to %v",
		s, new)
}

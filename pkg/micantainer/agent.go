package micantainer

import (
	"context"
	"mica-shim/pkg/fileutils"
	"mica-shim/pkg/libmica"
	"time"
)

const (
	maxHostnameLength = 64
)

type RealAgent struct {
}

// nolint:golint
func NewAgent() *RealAgent {
	return &RealAgent{}
}

// init initializes the Noop agent, i.e. it does nothing.
func (n *RealAgent) init(ctx context.Context, sandbox *Sandbox) (bool, error) {
	return false, nil
}

func (n *RealAgent) longLiveConn() bool {
	return false
}

// disconnect is the Noop agent connection closer. It does nothing.
func (n *RealAgent) disconnect(ctx context.Context) error {
	return nil
}

// stopSandbox is the Noop agent Sandbox stopping implementation. It does nothing.
func (n *RealAgent) stopSandbox(ctx context.Context, sandbox *Sandbox) error {
	if err := libmica.Stop(sandbox.id); err != nil {
		return err
	}
	return nil
}

// createSandbox creates a new sandbox by initializing MICA daemon
// TODO: crutial network setup
func (n *RealAgent) createSandbox(ctx context.Context, sandbox *Sandbox) error {
	return nil
}

// startSandbox starts the sandbox by booting RTOS clients
func (n *RealAgent) startSandbox(ctx context.Context, sandbox *Sandbox) error {
	// Start all containers in the sandbox
	for _, container := range sandbox.containers {
		if err := n.startContainer(sandbox, container); err != nil {
			return err
		}
	}
	return nil
}

// createContainer creates a new container in the sandbox
func (n *RealAgent) createContainer(ctx context.Context, sandbox *Sandbox, c *Container) (*RTOSTask, error) {
	// Create RTOS task through MICA daemon

	// task, err := libmica.(c.id, c.config.FirmwarePath, c.config.PedConfig.PedType)
	// if err != nil {
	// 	return nil, err
	// }
	// TODO: libmica
	shortId := fileutils.ShortID(c.ID())
	task := &RTOSTask{
		TaskID:       shortId,
		StartTime:    time.Now(),
		ReceiverAddr: 0x4500000,
	}

	return task, nil
}

// startContainer starts a specific container
func (n *RealAgent) startContainer(sandbox *Sandbox, c *Container) error {
	// Start the RTOS task
	if err := libmica.Start(c.id); err != nil {
		return err
	}
	// Update container state
	c.state.State = StateRunning
	return nil
}

// closeTaskStdin is the Noop agent process stdin closer. It does nothing.
// nolint
func (n *RealAgent) closeTaskStdin(ctx context.Context, c *Container, ProcessID string) error {
	return nil
}

// it is a temporary solution that merge stdout, stderr into ont output stream
func (n *RealAgent) readOut(ctx context.Context, c *Container, taskID string, data []byte) (int, error) {
	return 0, nil
}

// readTaskStdout is the Noop agent process stdout reader. It does nothing.
func (n *RealAgent) readTaskStdout(ctx context.Context, c *Container, taskID string, data []byte) (int, error) {
	return n.readOut(ctx, c, taskID, data)
}

// TODO: Calling ped methods
func (n *RealAgent) vcpuSet(ctx context.Context) (uint32, error) {
	return maxVCPUNumber(), nil
}

func (n *RealAgent) getDNS(s *Sandbox) ([]string, error) {
	ret := make([]string, 0)
	return ret, nil
}

// try to reorder resources dom0 can do, it cannot, just okay
func (n *RealAgent) Cleanup(ctx context.Context) {
}

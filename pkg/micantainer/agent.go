package micantainer

import (
	"context"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/libmica"
	"time"

	"micrun/pkg/cpuset"
)

const (
	maxHostnameLength = 64
)

type SandboxResource struct {
	// total vcpu number of sanbox
	VcpuNum uint32

	// TODO: Pool is not enabled currently
	// Physical cpu pool for container,
	PcpuPool []int

	// Vcpu number of each container
	ContainerVcpus map[string][]int
	// Cpuset of each container
	ContainerCpuSets map[string]cpuset.CPUSet
	// Total requested memory of sandbox workloads
	MemoryPoolBytes uint64
}

// nolint:golint
func NewAgent() *SandboxResource {
	return &SandboxResource{
		ContainerVcpus:   make(map[string][]int),
		ContainerCpuSets: make(map[string]cpuset.CPUSet),
	}
}

// init initializes the Noop agent, i.e. it does nothing.
func (n *SandboxResource) init(sandbox *Sandbox) (bool, error) {
	return false, nil
}

func (n *SandboxResource) longLiveConn() bool {
	return false
}

// disconnect is the Noop agent connection closer. It does nothing.
func (n *SandboxResource) disconnect() error {
	return nil
}

// stopClients is the Noop agent Sandbox stopping implementation. It does nothing.
func (n *SandboxResource) stopClients(ctx context.Context, sandbox *Sandbox) error {
	log.Infof("stopping client os in sandbox %s", sandbox.id)
	for _, c := range sandbox.containers {
		if err := c.stop(ctx, true); err != nil {
			log.Errorf("failed to stop container %s: %v", c.id, err)
			return err
		}
	}
	return nil
}

// createSandbox creates a new sandbox by initializing MICA daemon
// TODO: crutial network setup
func (n *SandboxResource) createSandbox(sandbox *Sandbox) error {
	return nil
}

// startSandbox starts the sandbox by booting RTOS clients
func (n *SandboxResource) startSandbox(sandbox *Sandbox) error {
	// Start all containers in the sandbox
	for _, container := range sandbox.containers {
		if err := n.startContainer(sandbox, container); err != nil {
			return err
		}
	}
	return nil
}

// createContainer creates a new container in the sandbox
func (n *SandboxResource) createContainer(sandbox *Sandbox, c *Container) (*RTOSTask, error) {
	// Create RTOS task through MICA daemon

	// task, err := libmica.(c.id, c.config.FirmwarePath, c.config.PedConfig.PedType)
	// if err != nil {
	// 	return nil, err
	// }
	// TODO: libmica
	task := &RTOSTask{
		TaskID:       c.ID(),
		CreateTime:   time.Now(),
		ReservedAddr: 0x1000,
	}

	return task, nil
}

// startContainer starts a specific container
func (n *SandboxResource) startContainer(sandbox *Sandbox, c *Container) error {
	// Start the RTOS task
	if err := libmica.Start(c.id); err != nil {
		return err
	}
	return c.setContainerState(c.ctx, StateRunning)
}

// closeContainerStdin is the Noop agent process stdin closer. It does nothing.
// nolint
func (n *SandboxResource) closeContainerStdin(c *Container) error {
	if c == nil || c.config == nil {
		return er.EmptyContainerID
	}
	if c.config.IsInfra {
		return nil
	}

	if c.id == "" {
		return er.EmptyContainerID
	}

	return nil
}

// it is a temporary solution that merge stdout, stderr into ont output stream
func (n *SandboxResource) readOut(c *Container, taskID string, data []byte) (int, error) {
	return 0, nil
}

// readTaskStdout is the Noop agent process stdout reader. It does nothing.
func (n *SandboxResource) readTaskStdout(c *Container, taskID string, data []byte) (int, error) {
	return n.readOut(c, taskID, data)
}

func (n *SandboxResource) resizeVCPUs(newNum uint32) (uint32, uint32) {
	old := n.VcpuNum
	n.VcpuNum = newNum
	return old, n.VcpuNum
}

func (n *SandboxResource) resizeMemory(newMemMB uint64) (uint64, uint64) {
	// Convert MiB to bytes
	newMem := newMemMB << 20
	old := n.MemoryPoolBytes
	if old == newMem {
		// No change; avoid unnecessary churn
		return old, old
	}
	n.MemoryPoolBytes = newMem
	return old, newMem
}

func (n *SandboxResource) getDNS(s *Sandbox) ([]string, error) {
	ret := make([]string, 0)
	return ret, nil
}

func (n *SandboxResource) getTotalMemoryMB() uint64 {
	return n.MemoryPoolBytes >> 20
}

// try to reorder resources dom0 can do, it cannot, just okay
func (n *SandboxResource) Cleanup() {
}

func (n *SandboxResource) ContainerVcpuSet(cid string) ([]int, error) {
	list, ok := n.ContainerVcpus[cid]
	if !ok {
		return []int{}, er.ContainerNotFound
	}

	return list, nil
}

func (n *SandboxResource) setNewPCpuList(cpulist []int) {
	n.PcpuPool = cpulist
}

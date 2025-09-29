package micantainer

import (
	er "mica-shim/errors"
	"mica-shim/pkg/libmica"
	"mica-shim/pkg/utils"
	"time"

	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/cpuset"
)

const (
	maxHostnameLength = 64
)

type SandboxAgent struct {
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
func NewAgent() *SandboxAgent {
	return &SandboxAgent{}
}

// init initializes the Noop agent, i.e. it does nothing.
func (n *SandboxAgent) init(sandbox *Sandbox) (bool, error) {
	return false, nil
}

func (n *SandboxAgent) longLiveConn() bool {
	return false
}

// disconnect is the Noop agent connection closer. It does nothing.
func (n *SandboxAgent) disconnect() error {
	return nil
}

// stopSandbox is the Noop agent Sandbox stopping implementation. It does nothing.
func (n *SandboxAgent) stopSandbox(sandbox *Sandbox) error {
	if err := libmica.Stop(sandbox.id); err != nil {
		return err
	}
	return nil
}

// createSandbox creates a new sandbox by initializing MICA daemon
// TODO: crutial network setup
func (n *SandboxAgent) createSandbox(sandbox *Sandbox) error {
	return nil
}

// startSandbox starts the sandbox by booting RTOS clients
func (n *SandboxAgent) startSandbox(sandbox *Sandbox) error {
	// Start all containers in the sandbox
	for _, container := range sandbox.containers {
		if err := n.startContainer(sandbox, container); err != nil {
			return err
		}
	}
	return nil
}

// createContainer creates a new container in the sandbox
func (n *SandboxAgent) createContainer(sandbox *Sandbox, c *Container) (*RTOSTask, error) {
	// Create RTOS task through MICA daemon

	// task, err := libmica.(c.id, c.config.FirmwarePath, c.config.PedConfig.PedType)
	// if err != nil {
	// 	return nil, err
	// }
	// TODO: libmica
	shortId := utils.ShortID(c.ID())
	task := &RTOSTask{
		TaskID:       shortId,
		StartTime:    time.Now(),
		ReceiverAddr: 0x4500000,
	}

	return task, nil
}

// startContainer starts a specific container
func (n *SandboxAgent) startContainer(sandbox *Sandbox, c *Container) error {
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
func (n *SandboxAgent) closeTaskStdin(c *Container, ProcessID string) error {
	return nil
}

// it is a temporary solution that merge stdout, stderr into ont output stream
func (n *SandboxAgent) readOut(c *Container, taskID string, data []byte) (int, error) {
	return 0, nil
}

// readTaskStdout is the Noop agent process stdout reader. It does nothing.
func (n *SandboxAgent) readTaskStdout(c *Container, taskID string, data []byte) (int, error) {
	return n.readOut(c, taskID, data)
}

func (n *SandboxAgent) resizeVCPUs(newNum uint32) (uint32, uint32) {
    old := n.VcpuNum
    n.VcpuNum = newNum
    return old, n.VcpuNum
}

func (n *SandboxAgent) resizeMemory(newMemMB uint64) (uint64, uint64) {
	newMem := newMemMB << 8
	old := n.MemoryPoolBytes
	n.MemoryPoolBytes = newMem
	return old, newMem
}

func (n *SandboxAgent) getDNS(s *Sandbox) ([]string, error) {
	ret := make([]string, 0)
	return ret, nil
}

func (n *SandboxAgent) getTotalMemoryMB() uint64 {
	return n.MemoryPoolBytes >> 20
}

// try to reorder resources dom0 can do, it cannot, just okay
func (n *SandboxAgent) Cleanup() {
}

func (n *SandboxAgent) ContainerVcpuSet(cid string) ([]int, error) {
	list, ok := n.ContainerVcpus[cid]
	if !ok {
		return []int{}, er.ErrContainerNotFound
	}

	return list, nil
}

func (n *SandboxAgent) setNewPCpuList(cpulist []int) {
	n.PcpuPool = cpulist
}

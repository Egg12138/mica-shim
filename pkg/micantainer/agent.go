package micantainer

import (
	"context"
	er "mica-shim/errors"
	log "mica-shim/logger"
	"mica-shim/pkg/libmica"
	"mica-shim/pkg/utils"
	"os"
	"path/filepath"
	"time"

	"mica-shim/pkg/cpuset"
)

const (
	maxHostnameLength = 64
	// End Of Transmission control characters
	eotAscii = 0x04
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

// stopClients is the Noop agent Sandbox stopping implementation. It does nothing.
func (n *SandboxAgent) stopClients(ctx context.Context, sandbox *Sandbox) error {
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
		CreateTime:   time.Now(),
		ReservedAddr: 0x1000,
	}

	return task, nil
}

// startContainer starts a specific container
func (n *SandboxAgent) startContainer(sandbox *Sandbox, c *Container) error {
	// Start the RTOS task
	if err := libmica.Start(c.id); err != nil {
		return err
	}
	return c.setContainerState(c.ctx, StateRunning)
}

// closeContainerStdin is the Noop agent process stdin closer. It does nothing.
// nolint
// closeContainerStdin signals EOF to the container's PTY input without tearing down output.
// Rationale: For a PTY, closing the file descriptor would also affect reads; sending EOF (Ctrl-D)
// is the conventional way to indicate stdin closure while allowing stdout to continue.
func (n *SandboxAgent) closeContainerStdin(c *Container) error {
	if c == nil || c.config == nil {
		return er.EmptyContainerID
	}
	if c.config.IsInfra {
		return nil
	}

	shortID := utils.ShortID(c.id)
	if shortID == "" {
		return er.EmptyContainerID
	}

	// mica create symlink /dev/ttyRPMSG_<shortID> -> /dev/pts/N;
	// it's better to resolve the link and write Ctrl-D to the pty slave.
	symlink := filepath.Clean("/dev/ttyRPMSG_" + shortID)
	target, err := filepath.EvalSymlinks(symlink)
	if err != nil {
		// Best-effort: in mock mode or legacy systems we may not have the symlink.
		return nil
	}
	f, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	_, _ = f.Write([]byte{eotAscii})
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
		return []int{}, er.ContainerNotFound
	}

	return list, nil
}

func (n *SandboxAgent) setNewPCpuList(cpulist []int) {
	n.PcpuPool = cpulist
}

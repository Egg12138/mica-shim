package micantainer

import (
	"context"
	"fmt"
	"mica-shim/pkg/fileutils"
	"mica-shim/pkg/libmica"
	"time"

	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/agent/protocols/grpc"
	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/types"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	maxHostnameLength = 64
)

type RealAgent struct {
}

// nolint:golint
func NewMockRealRealAgent() *RealAgent {
	return &RealAgent{}
}

// init initializes the Noop agent, i.e. it does nothing.
func (n *RealAgent) init(ctx context.Context, sandbox *Sandbox) (bool, error) {
	return false, nil
}

func (n *RealAgent) longLiveConn() bool {
	return false
}

// capabilities returns empty capabilities, i.e no capabilties are supported.
func (n *RealAgent) capabilities() types.Capabilities {
	return types.Capabilities{}
}

// disconnect is the Noop agent connection closer. It does nothing.
func (n *RealAgent) disconnect(ctx context.Context) error {
	return nil
}

// exec is the Noop agent command execution implementation. It does nothing.
func (n *RealAgent) exec(ctx context.Context, sandbox *Sandbox, c Container, cmd types.Cmd) (*RTOSTask, error) {
	return nil, nil
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
	hostname := sandbox.config.Hostname
	if len(hostname) > maxHostnameLength {
		hostname = hostname[:maxHostnameLength]
	}

	_, err := n.getDNS(sandbox)
	if err != nil {
		return err
	}

	if !sandbox.CheckDaemon().Active() {
		return fmt.Errorf("mica daemon is not listening or not started")
	}
	return nil
}

// startSandbox starts the sandbox by booting RTOS clients
func (n *RealAgent) startSandbox(ctx context.Context, sandbox *Sandbox) error {
	// Start all containers in the sandbox
	for _, container := range sandbox.containers {
		if err := n.startContainer(ctx, sandbox, container); err != nil {
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
func (n *RealAgent) startContainer(ctx context.Context, sandbox *Sandbox, c *Container) error {
	// Start the RTOS task
	if err := libmica.Start(c.id); err != nil {
		return err
	}
	// Update container state
	c.state.State = StateRunning
	return nil
}

// statsContainer gets statistics for a specific container
func (n *RealAgent) statsContainer(ctx context.Context, sandbox *Sandbox, c Container) (*ContainerStats, error) {
	// Get container stats from MICA daemon
	return &ContainerStats{}, nil
}

// waitTask waits for a task to complete
// TODO: Implement wait in libmica, and then use it here
// TODO: handling exit code
func (n *RealAgent) waitTask(ctx context.Context, c *Container, processID string) (int32, error) {
	// Wait for RTOS task to complete
	err := libmica.Stop(c.ID())
	if err != nil {
		return 0, err
	}

	return 0, nil
}

// getOOMEvent gets OOM events from the agent
func (n *RealAgent) getOOMEvent(ctx context.Context) (string, error) {
	// Get OOM events from MICA daemon
	return "", nil
}

// stopContainer stops a specific container
func (n *RealAgent) stopContainer(ctx context.Context, sandbox *Sandbox, c Container) error {
	return nil
}

// updateContainer is the Noop agent Container update implementation. It does nothing.
func (n *RealAgent) updateContainer(ctx context.Context, sandbox *Sandbox, c Container, resources specs.LinuxResources) error {
	return nil
}

// memHotplugByProbe is the Noop agent notify meomory hotplug event via probe interface implementation. It does nothing.
func (n *RealAgent) memHotplugByProbe(ctx context.Context, addr uint64, sizeMB uint32, memorySectionSizeMB uint32) error {
	return nil
}

// onlineCPUMem is the Noop agent Container online CPU and Memory implementation. It does nothing.
func (n *RealAgent) onlineCPUMem(ctx context.Context, cpus uint32, cpuOnly bool) error {
	return nil
}

// winsizeTask is the Noop agent process tty resizer. It does nothing.
func (n *RealAgent) winsizeTask(ctx context.Context, c *Container, processID string, height, width uint32) error {
	return nil
}

// writeTaskStdin is the Noop agent process stdin writer. It does nothing.
func (n *RealAgent) writeTaskStdin(ctx context.Context, c *Container, ProcessID string, data []byte) (int, error) {
	return 0, nil
}

// closeTaskStdin is the Noop agent process stdin closer. It does nothing.
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

// readTaskStderr is the Noop agent process stderr reader. It does nothing.
func (n *RealAgent) readTaskStderr(ctx context.Context, c *Container, taskID string, data []byte) (int, error) {
	return n.readOut(ctx, c, taskID, data)
}

// pauseContainer is the Noop agent Container pause implementation. It does nothing.
func (n *RealAgent) pauseContainer(ctx context.Context, sandbox *Sandbox, c Container) error {
	return nil
}

// resumeContainer is the Noop agent Container resume implementation. It does nothing.
func (n *RealAgent) resumeContainer(ctx context.Context, sandbox *Sandbox, c Container) error {
	return nil
}

// configure is the Noop agent configuration implementation. It does nothing.
func (n *RealAgent) configure(ctx context.Context) error {
	return nil
}

func (n *RealAgent) configureFromGrpc(ctx context.Context, id string) error {
	return nil
}

// reseedRNG is the Noop agent RND reseeder. It does nothing.
func (n *RealAgent) reseedRNG(ctx context.Context, data []byte) error {
	return nil
}

// reuseRealAgent is the Noop agent reuser. It does nothing.
func (n *RealAgent) reuseRealAgent(agent RealAgent) error {
	return nil
}

// getRealAgentURL is the Noop agent url getter. It returns nothing.
func (n *RealAgent) getRealAgentURL() (string, error) {
	return "", nil
}

// setRealAgentURL is the Noop agent url setter. It does nothing.
func (n *RealAgent) setRealAgentURL() error {
	return nil
}

// getGuestDetails is the Noop agent GuestDetails queryer. It does nothing.
func (n *RealAgent) getGuestDetails(context.Context, *grpc.GuestDetailsRequest) (*grpc.GuestDetailsResponse, error) {
	return nil, nil
}

// setGuestDateTime is the Noop agent guest time setter. It does nothing.
func (n *RealAgent) setGuestDateTime(context.Context, time.Time) error {
	return nil
}

// copyFile is the Noop agent copy file. It does nothing.
func (n *RealAgent) copyFile(ctx context.Context, src, dst string) error {
	return nil
}

// addSwap is the Noop agent setup swap. It does nothing.
func (n *RealAgent) addSwap(ctx context.Context, PCIPath types.PciPath) error {
	return nil
}

func (n *RealAgent) markDead(ctx context.Context) {
}

func (n *RealAgent) cleanup(ctx context.Context) {
}

func (n *RealAgent) getRealAgentMetrics(ctx context.Context, req *grpc.GetMetricsRequest) (*grpc.Metrics, error) {
	return nil, nil
}

func (n *RealAgent) getGuestVolumeStats(ctx context.Context, volumeGuestPath string) ([]byte, error) {
	return nil, nil
}

func (n *RealAgent) resizeGuestVolume(ctx context.Context, volumeGuestPath string, size uint64) error {
	return nil
}

func (n *RealAgent) getIPTables(ctx context.Context, isIPv6 bool) ([]byte, error) {
	return nil, nil
}

func (n *RealAgent) setIPTables(ctx context.Context, isIPv6 bool, data []byte) error {
	return nil
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
	return
}

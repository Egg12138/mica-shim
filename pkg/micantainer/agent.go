package micantainer

import (
	"context"
	"mica-shim/pkg/libmica"
	"syscall"
	"time"

	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/agent/protocols/grpc"
	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/types"
	"github.com/opencontainers/runtime-spec/specs-go"
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

// createSandbox is the Noop agent sandbox creation implementation. It does nothing.
func (n *RealAgent) createSandbox(ctx context.Context, sandbox *Sandbox) error {
	return nil
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

// startSandbox is the Noop agent Sandbox starting implementation. It does nothing.
func (n *RealAgent) startSandbox(ctx context.Context, sandbox *Sandbox) error {
	return nil
}

// stopSandbox is the Noop agent Sandbox stopping implementation. It does nothing.
func (n *RealAgent) stopSandbox(ctx context.Context, sandbox *Sandbox) error {
	if err := libmica.Stop(sandbox.id); err != nil {	
		return err
	}
	return nil
}

// createContainer is the Noop agent Container creation implementation. It does nothing.
func (n *RealAgent) createContainer(ctx context.Context, sandbox *Sandbox, c *Container) (*RTOSTask, error) {
	return &RTOSTask{}, nil
}

// startContainer is the Noop agent Container starting implementation. It does nothing.
func (n *RealAgent) startContainer(ctx context.Context, sandbox *Sandbox, c *Container) error {
	return nil
}

// stopContainer is the Noop agent Container stopping implementation. It does nothing.
func (n *RealAgent) stopContainer(ctx context.Context, sandbox *Sandbox, c Container) error {
	if err := libmica.Stop(c.ID()); err != nil {
		return err
	}
	return nil
}

// signalProcess is the Noop agent Container signaling implementation. It does nothing.
func (n *RealAgent) signalProcess(ctx context.Context, c *Container, processID string, signal syscall.Signal, all bool) error {
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

// check is the Noop agent health checker. It does nothing.
func (n *RealAgent) check(ctx context.Context) error {
	return nil
}

// statsContainer is the Noop agent Container stats implementation. It does nothing.
func (n *RealAgent) statsContainer(ctx context.Context, sandbox *Sandbox, c Container) (*ContainerStats, error) {
	return &ContainerStats{}, nil
}

// waitTask is the Noop agent process waiter. It does nothing.
func (n *RealAgent) waitTask(ctx context.Context, c *Container, processID string) (int32, error) {
	return 0, nil
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

// readTaskStdout is the Noop agent process stdout reader. It does nothing.
func (n *RealAgent) readTaskStdout(ctx context.Context, c *Container, processID string, data []byte) (int, error) {
	return 0, nil
}

// readTaskStderr is the Noop agent process stderr reader. It does nothing.
func (n *RealAgent) readTaskStderr(ctx context.Context, c *Container, processID string, data []byte) (int, error) {
	return 0, nil
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


func (n *RealAgent) getOOMEvent(ctx context.Context) (string, error) {
	return "", nil
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

func (k *RealAgent) getIPTables(ctx context.Context, isIPv6 bool) ([]byte, error) {
	return nil, nil
}

func (k *RealAgent) setIPTables(ctx context.Context, isIPv6 bool, data []byte) error {
	return nil
}

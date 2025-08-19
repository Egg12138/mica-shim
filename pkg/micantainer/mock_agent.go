package micantainer

import (
	"context"
	"syscall"
	"time"

	persistapi "github.com/kata-containers/kata-containers/src/runtime/virtcontainers/persist/api"
	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/agent/protocols/grpc"
	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/types"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// TALK： 在micran层面，我们有时候需要适当跳过mica,来直接通过Xen对rtos传输信息；
// 未来整合的时候，都作为mica 一部分
// MockAgent 本身始终在 host中
// 我们需要一个通用的 MockAgent 设计策略 来：
//  1. 管理 rtos 的devices, net, tasks...
//  2. 在agent中处理IO吗？
//     3.
// NOTICE: here agent is a placeholder
type MockAgent struct {
}


// nolint:golint
func NewMockAgent() *MockAgent {
	return &MockAgent{}
}

// init initializes the Noop agent, i.e. it does nothing.
func (n *MockAgent) init(ctx context.Context, sandbox *Sandbox) (bool, error) {
	return false, nil
}

func (n *MockAgent) longLiveConn() bool {
	return false
}

// createSandbox is the Noop agent sandbox creation implementation. It does nothing.
func (n *MockAgent) createSandbox(ctx context.Context, sandbox *Sandbox) error {
	return nil
}

// capabilities returns empty capabilities, i.e no capabilties are supported.
func (n *MockAgent) capabilities() types.Capabilities {
	return types.Capabilities{}
}

// disconnect is the Noop agent connection closer. It does nothing.
func (n *MockAgent) disconnect(ctx context.Context) error {
	return nil
}

// exec is the Noop agent command execution implementation. It does nothing.
func (n *MockAgent) exec(ctx context.Context, sandbox *Sandbox, c Container, cmd types.Cmd) (*RTOSTask, error) {
	return nil, nil
}

// startSandbox is the Noop agent Sandbox starting implementation. It does nothing.
func (n *MockAgent) startSandbox(ctx context.Context, sandbox *Sandbox) error {
	return nil
}

// stopSandbox is the Noop agent Sandbox stopping implementation. It does nothing.
func (n *MockAgent) stopSandbox(ctx context.Context, sandbox *Sandbox) error {
	return nil
}

// createContainer is the Noop agent Container creation implementation. It does nothing.
func (n *MockAgent) createContainer(ctx context.Context, sandbox *Sandbox, c *Container) (*RTOSTask, error) {
	return &RTOSTask{}, nil
}

// startContainer is the Noop agent Container starting implementation. It does nothing.
func (n *MockAgent) startContainer(ctx context.Context, sandbox *Sandbox, c *Container) error {
	return nil
}

// stopContainer is the Noop agent Container stopping implementation. It does nothing.
func (n *MockAgent) stopContainer(ctx context.Context, sandbox *Sandbox, c Container) error {
	return nil
}

// signalProcess is the Noop agent Container signaling implementation. It does nothing.
func (n *MockAgent) signalProcess(ctx context.Context, c *Container, processID string, signal syscall.Signal, all bool) error {
	return nil
}

// updateContainer is the Noop agent Container update implementation. It does nothing.
func (n *MockAgent) updateContainer(ctx context.Context, sandbox *Sandbox, c Container, resources specs.LinuxResources) error {
	return nil
}

// memHotplugByProbe is the Noop agent notify meomory hotplug event via probe interface implementation. It does nothing.
func (n *MockAgent) memHotplugByProbe(ctx context.Context, addr uint64, sizeMB uint32, memorySectionSizeMB uint32) error {
	return nil
}

// onlineCPUMem is the Noop agent Container online CPU and Memory implementation. It does nothing.
func (n *MockAgent) onlineCPUMem(ctx context.Context, cpus uint32, cpuOnly bool) error {
	return nil
}

// check is the Noop agent health checker. It does nothing.
func (n *MockAgent) check(ctx context.Context) error {
	return nil
}

// statsContainer is the Noop agent Container stats implementation. It does nothing.
func (n *MockAgent) statsContainer(ctx context.Context, sandbox *Sandbox, c Container) (*ContainerStats, error) {
	return &ContainerStats{}, nil
}

// waitTask is the Noop agent process waiter. It does nothing.
func (n *MockAgent) waitTask(ctx context.Context, c *Container, processID string) (int32, error) {
	return 0, nil
}

// winsizeTask is the Noop agent process tty resizer. It does nothing.
func (n *MockAgent) winsizeTask(ctx context.Context, c *Container, processID string, height, width uint32) error {
	return nil
}

// writeTaskStdin is the Noop agent process stdin writer. It does nothing.
func (n *MockAgent) writeTaskStdin(ctx context.Context, c *Container, ProcessID string, data []byte) (int, error) {
	return 0, nil
}

// closeTaskStdin is the Noop agent process stdin closer. It does nothing.
func (n *MockAgent) closeTaskStdin(ctx context.Context, c *Container, ProcessID string) error {
	return nil
}

// readTaskStdout is the Noop agent process stdout reader. It does nothing.
func (n *MockAgent) readTaskStdout(ctx context.Context, c *Container, processID string, data []byte) (int, error) {
	return 0, nil
}

// readTaskStderr is the Noop agent process stderr reader. It does nothing.
func (n *MockAgent) readTaskStderr(ctx context.Context, c *Container, processID string, data []byte) (int, error) {
	return 0, nil
}

// pauseContainer is the Noop agent Container pause implementation. It does nothing.
func (n *MockAgent) pauseContainer(ctx context.Context, sandbox *Sandbox, c Container) error {
	return nil
}

// resumeContainer is the Noop agent Container resume implementation. It does nothing.
func (n *MockAgent) resumeContainer(ctx context.Context, sandbox *Sandbox, c Container) error {
	return nil
}

// configure is the Noop agent configuration implementation. It does nothing.
func (n *MockAgent) configure(ctx context.Context) error {
	return nil
}

func (n *MockAgent) configureFromGrpc(ctx context.Context, id string) error {
	return nil
}

// reseedRNG is the Noop agent RND reseeder. It does nothing.
func (n *MockAgent) reseedRNG(ctx context.Context, data []byte) error {
	return nil
}

// reuseAgent is the Noop agent reuser. It does nothing.
func (n *MockAgent) reuseAgent(agent MockAgent) error {
	return nil
}

// getAgentURL is the Noop agent url getter. It returns nothing.
func (n *MockAgent) getAgentURL() (string, error) {
	return "", nil
}

// setAgentURL is the Noop agent url setter. It does nothing.
func (n *MockAgent) setAgentURL() error {
	return nil
}

// getGuestDetails is the Noop agent GuestDetails queryer. It does nothing.
func (n *MockAgent) getGuestDetails(context.Context, *grpc.GuestDetailsRequest) (*grpc.GuestDetailsResponse, error) {
	return nil, nil
}

// setGuestDateTime is the Noop agent guest time setter. It does nothing.
func (n *MockAgent) setGuestDateTime(context.Context, time.Time) error {
	return nil
}

// copyFile is the Noop agent copy file. It does nothing.
func (n *MockAgent) copyFile(ctx context.Context, src, dst string) error {
	return nil
}

// addSwap is the Noop agent setup swap. It does nothing.
func (n *MockAgent) addSwap(ctx context.Context, PCIPath types.PciPath) error {
	return nil
}

func (n *MockAgent) markDead(ctx context.Context) {
}

func (n *MockAgent) cleanup(ctx context.Context) {
}

// save is the Noop agent state saver. It does nothing.
func (n *MockAgent) save() (s persistapi.AgentState) {
	return
}

// load is the Noop agent state loader. It does nothing.
func (n *MockAgent) load(s persistapi.AgentState) {}

func (n *MockAgent) getOOMEvent(ctx context.Context) (string, error) {
	return "", nil
}

func (n *MockAgent) getAgentMetrics(ctx context.Context, req *grpc.GetMetricsRequest) (*grpc.Metrics, error) {
	return nil, nil
}

func (n *MockAgent) getGuestVolumeStats(ctx context.Context, volumeGuestPath string) ([]byte, error) {
	return nil, nil
}

func (n *MockAgent) resizeGuestVolume(ctx context.Context, volumeGuestPath string, size uint64) error {
	return nil
}

func (k *MockAgent) getIPTables(ctx context.Context, isIPv6 bool) ([]byte, error) {
	return nil, nil
}

func (k *MockAgent) setIPTables(ctx context.Context, isIPv6 bool, data []byte) error {
	return nil
}

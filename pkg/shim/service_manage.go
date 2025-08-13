package shim

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	log "mica-shim/logger"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/oci"

	"github.com/containerd/containerd/api/events"
	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/namespaces"
	cdruntime "github.com/containerd/containerd/runtime"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/types/known/timestamppb"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

// github.com/containerd/containerd/api/events/task.pb.go
// TaskExit: ContainerID => cid, ID => id, Pid
type exit struct {
	ts  time.Time
	cid string
	// always a dummy
	execid string
	// always a dummy
	pid    uint32
	status int
}

type shimService struct {
	sandbox    *cntr.SandboxTraits
	containers map[string]*cntr.Container
	shimPid    uint32
	// context:
	ctx context.Context
	// shutdown will be initialized as shutdown.Shutdown, with context,
	// inside shim.run so that no need to setup shutdown service manually.
	ss func()
	// events:
	events  chan any
	monitor chan error
	ec      chan exit
	// configs
	config    *oci.RuntimeConfig
	namespace string
	id        string
	// sync:
	mu          sync.Mutex
	eventSendMu sync.Mutex
}

var (
	_ taskAPI.TaskService = (*shimService)(nil)
)

const (
	channelSize = 128
)

func setupDevLog() {
	if err := log.CleanDebugFile(); err != nil {
		log.Errorf("failed to clean debug file: %v", err)
	}
	log.Debugf("args: %s", os.Args)
}

func newCommand(ctx context.Context, opts shimv2.StartOpts, cwd string) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get current executable path: %w", err)
	}

	var args []string
	if opts.Debug {
		args = append(args, "-debug")
		args = append(args, "-id", opts.ID)
	}

	// TTRPC_ADDRESS the address of containerd's ttrpc API socket
	// GRPC_ADDRESS the address of containerd's grpc API socket (1.7+)
	// MAX_SHIM_VERSION the maximum shim version supported by the client, always 2 for shim v2 (1.7+)
	// SCHED_CORE enable core scheduling if available (1.6+)
	// NAMESPACE an optional namespace the shim is operating in or inheriting (1.7+)
	cmdCfg := &shimv2.CommandConfig{
		Runtime:      self,
		Address:      opts.Address,
		TTRPCAddress: opts.TTRPCAddress,
		// resolved expanded path
		Path:      cwd,
		SchedCore: os.Getenv(contdShimEnvShedCore) != "",
		Args:      args,
	}

	// -namespace the namespace for the container
	// -address the address of the containerd's main grpc socket
	// -publish-binary the binary path to publish events back to containerd
	// -id the id of the container (containerID)
	// The start command, as well as all binary calls to the shim, has the bundle for the container set as the cwd.
	cmd, err := shimv2.Command(ctx, cmdCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create shim command: %w", err)
	}
	return cmd, nil
}

func New(ctx context.Context, id string, publisher shimv2.Publisher, shutdown func()) (shimv2.Shim, error) {
	ns, found := namespaces.Namespace(ctx)
	if !found {
		return nil, fmt.Errorf("namespace is required")
	}
	s := &shimService{
		id:        id,
		namespace: ns,
		shimPid:   uint32(os.Getpid()),
		ctx:       ctx,
		events:    make(chan any, channelSize),
		ec:        make(chan exit, channelSize),
		ss:        shutdown,
		monitor:   make(chan error),
	}

	go s.listenAndReportExits()

	return s, nil
}

// Containerd:
//   - Shim server interface
//   - (2.0): Remove unified shim interface
//   - type Shim interface {
//     shimapi.TaskService
//     Cleanup(ctx context.Context) (*shimapi.DeleteResponse, error)
//     StartShim(ctx context.Context, opts StartOpts) (string, error)
//     }
func (s *shimService) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	if s.id == "" {
		return nil, fmt.Errorf("container ID is required")
	}

	ociSpec, err := oci.ParseConfigJSON(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to load valid runtime config: %w", err)
	}

	ctype, err := oci.GetContainerType(&ociSpec)
	if err != nil {
		return nil, err
	}

	switch ctype {
	case cntr.PodSandbox, cntr.SingleContainer:
		err = cleanupContainer(ctx, s.id, s.id, cwd)
		if err != nil {
			return nil, err
		}
	case cntr.PodContainer:
		sandboxID, err := oci.GetSandboxID(&ociSpec)
		if err != nil {
			return nil, err
		}
		err = cleanupContainer(ctx, sandboxID, s.id, cwd)
		if err != nil {
			return nil, err
		}
	default:
		log.Infof("unknown container type to be cleaned up: %s", ctype)
	}

	return &taskAPI.DeleteResponse{
		ExitedAt:   timestamppb.New(time.Now()),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil

}

// Cleanup a Container instance from a pod
func cleanupContainer(ctx context.Context, sandboxID, containerID, bundle string) error {
	return cntr.CleanupContainer(ctx, sandboxID, containerID, false)
}

func (s *shimService) StartShim(ctx context.Context, opts shimv2.StartOpts) (_ string, retErr error) {
	bundle, err := os.Getwd()
	if err != nil {
		return "", err
	}

	bundle, err = validBundle(opts.ID, bundle)
	if err != nil {
		return "", err
	}

	sockaddr, err := preparePodSocketAddr(ctx, bundle, opts)
	if err != nil {
		return "", err
	}

	// if podContainer: do not need a new shim binary, only write socket and then finished starting
	if sockaddr != "" {
		// write <socketaddr> into <bundle>/address socket
		if err := shimv2.WriteAddress("address", sockaddr); err != nil {
			return "", fmt.Errorf("failed to write socket address for pod container: %w", err)
		}
		return sockaddr, nil
	}

	setupDevLog()
	cmd, err := newCommand(ctx, opts, bundle)
	if err != nil {
		return "", err
	}

	// single container / sandbox
	sockAddr, err := shimv2.SocketAddress(ctx, opts.Address, opts.ID)
	if err != nil {
		return "", err
	}

	socket, err := shimv2.NewSocket(sockAddr)

	if err != nil {
		switch {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		case !shimv2.SocketEaddrinuse(err):
			return "", fmt.Errorf("socket address already in use: %w", err)

		case shimv2.CanConnect(sockAddr):
			if err := shimv2.WriteAddress("address", sockAddr); err != nil {
				return "", fmt.Errorf("failed to write sandbox/regular container socket address: %w", err)
			}
			return sockAddr, nil
		}

		if err := shimv2.RemoveSocket(sockAddr); err != nil {
			return "", fmt.Errorf("failed to remove pre-existing shim socket: %w", err)
		}

		if socket, err = shimv2.NewSocket(sockAddr); err != nil {
			return "", fmt.Errorf("failed to create new shim socket: %w", err)
		}
	}

	defer func() {
		if retErr != nil {
			if err := socket.Close(); err != nil {
				log.Errorf("failed to close shim socket on start error: %v", err)
			}
			if err := shimv2.RemoveSocket(sockAddr); err != nil {
				log.Errorf("failed to remove shim socket on start error: %v", err)
			}
		}
	}()

	sockF, err := socket.File()
	if err != nil {
		return "", fmt.Errorf("failed to get shim socket file descriptor: %w", err)
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, sockF)

	runtime.LockOSThread()
	if os.Getenv("SCHED_CORE") != "" {
		log.Debugf("enable sched_core features")
		handleSchedCore()
	}

	if err := cmd.Start(); err != nil {
		sockF.Close()
		return "", fmt.Errorf("failed to start shim command: %w", err)
	}

	runtime.UnlockOSThread()

	defer func() {
		if retErr != nil {
			cmd.Process.Kill()
		}
	}()

	if err := shimv2.WritePidFile("shim.pid", cmd.Process.Pid); err != nil {
		return "", fmt.Errorf("failed to write shim PID file: %w", err)
	}

	return sockAddr, nil

}

func getTopic(e interface{}) string {
	log.Debugf("topic event: %v", e)
	switch e.(type) {
	case *events.TaskCreate:
		return cdruntime.TaskCreateEventTopic
	case *events.TaskStart:
		return cdruntime.TaskStartEventTopic
	case *events.TaskOOM:
		return cdruntime.TaskOOMEventTopic
	case *events.TaskExit:
		return cdruntime.TaskExitEventTopic
	case *events.TaskDelete:
		return cdruntime.TaskDeleteEventTopic
	case *events.TaskExecAdded:
		return cdruntime.TaskExecAddedEventTopic
	case *events.TaskExecStarted:
		return cdruntime.TaskExecStartedEventTopic
	case *events.TaskPaused:
		return cdruntime.TaskPausedEventTopic
	case *events.TaskResumed:
		return cdruntime.TaskResumedEventTopic
	case *events.TaskCheckpointed:
		return cdruntime.TaskCheckpointedEventTopic
	default:
		log.Warnf("no topic for event type: %v", e)
	}
	return cdruntime.TaskUnknownTopic
}

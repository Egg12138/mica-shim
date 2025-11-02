package shim

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/pkg/errors"

	log "mica-shim/logger"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/oci"

	"github.com/containerd/containerd/api/events"
	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/namespaces"
	cdruntime "github.com/containerd/containerd/runtime"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/types/known/timestamppb"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

// exit represents a container exit event.
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
	sandbox    cntr.SandboxTraits
	containers map[string]*container
	micadPid   uint32
	// context:
	ctx context.Context
	// shutdown will be initialized as shutdown.Shutdown, with context,
	// inside shim.run so that no need to setup shutdown service manually.
	ss func()
	// events:
	events chan any
	// TODO: future -> implement sandbox monitor
	monitor chan error
	ec      chan exit
	// configs
	config    *oci.RuntimeConfig
	namespace string
	// sandbox container id
	id string
	// sync:
	mu          sync.Mutex
	eventSendMu sync.Mutex
}

var (
	_ taskAPI.TaskService = (*shimService)(nil)

	// shimPid is the process ID of the shim.
	// It's initialized once when the package is loaded.
	shimPid = uint32(os.Getpid())
)

const (
	channelSize = 128
)

func New(ctx context.Context, id string, publisher shimv2.Publisher, shutdown func()) (shimv2.Shim, error) {
	ns, found := namespaces.Namespace(ctx)
	if !found {
		return nil, fmt.Errorf("namespace is required")
	}
	log.Debugf("got namespace: %s", ns)
	micadPid, err := getMicadPid()
	if err != nil {
		log.Warnf("failed to get micad PID, setting to 0: %v", err)
		return nil, err
	}
	log.Debugf("got micadPid: %d", micadPid)

	s := &shimService{
		id:         id,
		micadPid:   micadPid,
		namespace:  ns,
		ctx:        ctx,
		containers: make(map[string]*container),
		events:     make(chan any, channelSize),
		ec:         make(chan exit, channelSize),
		ss:         shutdown,
		monitor:    make(chan error),
	}

	log.Debugf("starting service background goroutines exit listener")
	go s.listenAndReportExits()

	// Start events forwarder to publish events to containerd
	forwarder := s.newEventsForwarder(ctx, publisher)
	go forwarder.forward()

	log.Debugf("completed successfully, returning shimService")
	return s, nil
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
	// LOG_COLOR controls colored output in the shim process
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
	// Pass LOG_COLOR environment variable to child process
	if logColor := os.Getenv("LOG_COLOR"); logColor != "" {
		log.Debug("++++++++ colored log")
		cmd.Env = append(cmd.Env, "LOG_COLOR="+logColor)
	}

	// Do not redirect child's stdout here. The parent `start` path is
	// responsible for emitting the address cleanly; child's logging(info,warn,error) is
	// routed to containerd via the shim FIFO logger setup.
	return cmd, nil
}

// Cleanup handles container cleanup operations for different container types.
func (s *shimService) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	if s.id == "" {
		return nil, fmt.Errorf("container ID is required")
	}

	ociSpec, err := oci.LoadSpec(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to load valid runtime config: %w", err)
	}

	ctype, err := oci.GetContainerType(&ociSpec)
	if err != nil {
		return nil, err
	}

	log.Debugf("container type: %s, trying to cleanup it", ctype)
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
		log.Debugf("unknown container type to be cleaned up: %s", ctype)
	}

	return &taskAPI.DeleteResponse{
		ExitedAt:   timestamppb.New(time.Now()),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil

}

func cleanupContainer(ctx context.Context, sandboxID, containerID, bundle string) error {
	log.Debugf("cleanup container from sandbox %s, and remove rootfs of container %s", sandboxID, containerID)
	if err := cntr.CleanupContainer(ctx, sandboxID, containerID, false); err != nil {
		return fmt.Errorf("failed to cleanup container %s: %w", containerID, err)
	}

	rootfs := filepath.Join(bundle, "rootfs")
	if err := mount.UnmountAll(rootfs, 0); err != nil {
		log.Errorf("failed to umount: %s", rootfs)
		return err
	}
	return nil
}

func (s *shimService) StartShim(ctx context.Context, opts shimv2.StartOpts) (_ string, retErr error) {
	origLevel := log.Log.GetLevel()
	log.Log.SetLevel(logrus.WarnLevel)
	defer log.Log.SetLevel(origLevel)

	log.Debugf("startshim() being called")
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

	log.Debugf("args: %s", os.Args)
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
		log.Debugf("socket %s creation failed: %v", sockAddr, err)

		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !shimv2.SocketEaddrinuse(err) {
			return "", errors.Wrap(err, "create new socket")
		}

		if shimv2.CanConnect(sockAddr) {
			if err := shimv2.WriteAddress("address", sockAddr); err != nil {
				return "", errors.Wrap(err, "write existing socket for shim")
			}
			return sockAddr, nil
		}

		log.Debugf("removing stale socket and creating new one")
		if err := shimv2.RemoveSocket(sockAddr); err != nil {
			return "", errors.Wrap(err, "remove pre-existing socket")
		}
		if socket, err = shimv2.NewSocket(sockAddr); err != nil {
			return "", errors.Wrap(err, "try create new shim socket second time")
		}
	}

	defer func() {
		if retErr != nil {
			if err := socket.Close(); err != nil {
				log.Warnf("failed to close shim socket: %v", err)
			}
			if err := shimv2.RemoveSocket(sockAddr); err != nil {
				log.Warnf("failed to remove shim socket: %v", err)
			}
		}
	}()

	// make sure that reexec shim-v2 binary use the value if need
	if err := shimv2.WriteAddress("address", sockAddr); err != nil {
		return "", errors.Wrap(err, "write shim address file")
	}

	sock, err := socket.File()
	if err != nil {
		return "", fmt.Errorf("failed to get shim socket file descriptor: %w", err)
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, sock)

	runtime.LockOSThread()
	if os.Getenv("SCHED_CORE") != "" {
		log.Debugf("enable sched_core features")
		handleSchedCore()
	}

	if err := cmd.Start(); err != nil {
		_ = sock.Close()
		return "", fmt.Errorf("failed to start shim command: %w", err)
	}

	runtime.UnlockOSThread()

	defer func() {
		if retErr != nil {
			cmd.Process.Kill()
		}
	}()

	// Wait in background to avoid zombie if parent outlives child briefly.
	go cmd.Wait()

	if err = shimv2.WritePidFile("shim.pid", cmd.Process.Pid); err != nil {
		return "", fmt.Errorf("failed to write shim PID file: %w", err)
	}

	if err = shimv2.WriteAddress("address", sockAddr); err != nil {
		return "", err
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

// eventsForwarder handles forwarding events from the shim to containerd.
type eventsForwarder struct {
	service   *shimService
	context   context.Context
	publisher shimv2.Publisher
}

// newEventsForwarder creates a new events forwarder
func (s *shimService) newEventsForwarder(ctx context.Context, publisher shimv2.Publisher) *eventsForwarder {
	return &eventsForwarder{
		service:   s,
		context:   ctx,
		publisher: publisher,
	}
}

// forward listens for events and publishes them to containerd/isulad
func (ef *eventsForwarder) forward() {
	for e := range ef.service.events {
		topic := getTopic(e)
		if topic == cdruntime.TaskUnknownTopic {
			log.Warnf("unknown event type, skipping: %v", e)
			continue
		}

		// Publish the event to containerd
		ctx, cancel := context.WithTimeout(ef.context, timeOut)
		if err := ef.publisher.Publish(ctx, topic, e); err != nil {
			log.Errorf("failed to publish event topic=%s: %v", topic, err)
		} else {
			log.Debugf("Successfully forwarded event topic=%s", topic)
		}
		cancel()
	}
}

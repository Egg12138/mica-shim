package entry

import (
	"context"
	"fmt"
	"sync"

	defs "mica-shim/definitions"
	cntr "mica-shim/pkg/container"

	core "mica-shim/pkg/oci"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/pkg/shutdown"
	"github.com/containerd/containerd/runtime/v2/shim"
	"github.com/containerd/ttrpc"
)

// shutdown.Service is used to facilitate shutdown by through callback
func newTaskService(ss shutdown.Service) (*micaTaskService, error) {
	s := &micaTaskService{
		// procs:     make(Micantainers, 1),
		procs: make(initProcByTaskID, 1),
		ss:    ss,
	}

	sockAddr, err := shim.ReadAddress("address")
	if err != nil {
		return nil, fmt.Errorf("reading socket address from address file: %w", err)
	}

	ss.RegisterCallback(func(context.Context) error {
		if err := shim.RemoveSocket(sockAddr); err != nil {
			return fmt.Errorf("removing shim socket on shutdown: %w", err)
		}
		return nil
	})

	// ss.RegisterCallback(func(ctx context.Context) error {
	// 	if sockAddr, err := shim.ReadAddress("address"); err == nil {
	// 		if err := shim.RemoveSocket(sockAddr); err != nil {
	// 			log.Errorf("removing shim socket on shutdown: %v", err)
	// 			return fmt.Errorf("removing shim socket on shutdown: %w", err)
	// 		}
	// 	}
	// 	// Don't fail shutdown if address file doesn't exist or socket removal fails
	// 	return nil
	// })

	return s, nil
}

// Micantainers maps init (parent) processes to their associated task by ID.
type Micantainers map[string]*cntr.Container
type initProcByTaskID map[string]*initProcess

// initProcess encapsulates information about an init (parent) process.
// TODO: handle the init process, there it is just a placeholder
// TALK: init process **represent** the process in RTOS
// type initProcess struct {
// 	// TALK: for one rtos container, make agent process(in Linux) as the init process?
// 	pid        int
// 	doneCtx    context.Context
// 	doneCancel context.CancelFunc
// 	exitTime   time.Time
// 	exitStatus int
// 	stdout     string
// 	micaIO     *libmica.MicaIO // IO handler for PTY communication
// }

// deprecated: will be removed, replaced by shimService
// micaTaskService is an implementation of a containerd taskAPI.TaskService
// which prints the current time at regular intervals.
type micaTaskService struct {
	m sync.RWMutex
	// procs Micantainers
	procs map[string]*initProcess
	// namespace string
	ss shutdown.Service
}

type shimService struct {
	ctx        context.Context
	containers Micantainers
	namespace  string
	events     chan interface{}
	cancel     func()
	config     *core.RuntimeConfig
	// TODO: pedRuntimeInfo is a placeholder for now
	pedRuntimeInfo uint32
	shimPid        uint32
	eventSendMutex sync.Mutex
	m              sync.Mutex
}

var (
	_ taskAPI.TaskService = (*micaTaskService)(nil)
	_ taskAPI.TaskService = (*shimService)(nil)
)

// RegisterTTRPC registers this TTRPC service with the given TTRPC server.
func (s *micaTaskService) RegisterTTRPC(srv *ttrpc.Server) error {
	taskAPI.RegisterTaskService(srv, s)
	return nil
}

func (s *shimService) RegisterTTRPC(srv *ttrpc.Server) error {
	taskAPI.RegisterTaskService(srv, s)
	return nil
}

// TODO: expand mica response system
func success(response string) bool {
	return response == defs.MicaSuccess
}

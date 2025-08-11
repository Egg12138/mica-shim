package shim

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/oci"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/shutdown"
	"github.com/golang/protobuf/ptypes"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

// github.com/containerd/containerd/api/events/task.pb.go
// TaskExit: ContainerID => cid, ID => id, Pid
type exit struct {
	ts time.Time
	cid string
	// always a dummy
	execid string
	// always a dummy
	pid uint32
	status int
}

type shimService struct {
	sandbox *cntr.Sandbox
	containers map[string]*cntr.Container
	shimPid uint32
	// context:
	ctx context.Context
	// shutdown will be initialized as shutdown.Shutdown, with context, 
	// inside shim.run so that no need to setup shutdown service manually.
	ss func()
	// events:
	events chan any
	monitor chan error
	ec chan exit
	// configs
	config *oci.RuntimeConfig
	namespace string
	id string
	// sync:
	mu 					sync.Mutex
	eventSendMu sync.Mutex
}


var (
	_ taskAPI.TaskService = (*shimService)(nil)
)

func newTaskService(ss shutdown.Service) (*shimService, error) {
	s := &shimService{
		ss: ss,
		containers: make(map[string]*cntr.Container),
		shimPid: 0,
		ctx: nil,
		events: make(chan any),
		monitor: make(chan error),
		ec: make(chan exit),
	}

	sockAddr, err := shimv2.ReadAddress("address")
	if err != nil {
		return nil, fmt.Errorf("reading socket address from address file: %w", err)
	}

	ss.RegisterCallback(func(context.Context) error {
		if err := shimv2.RemoveSocket(sockAddr); err != nil {
			return fmt.Errorf("removing shim socket on shutdown: %w", err)
		}
		return nil
	})

	return s, nil
}

const (
	channelSize = 128
)

func New(ctx context.Context, id string, publisher shimv2.Publisher, shutdown func()) (shimv2.Shim, error) {
	ns, found := namespaces.Namespace(ctx)
	if !found {
		return nil, fmt.Errorf("namespace cannot be empty")
	}
	s := &shimService{
		id: 				id,
		namespace: 	ns,
		shimPid: 		uint32(os.Getpid()),
		ctx: 				ctx,
		events: 		make(chan any, channelSize),
		ec: 				make(chan exit, channelSize),
		ss: 				shutdown,
		monitor: 		make(chan error),
	}

	go s.listenAndReportExits()

	return s, nil
}


func (s *shimService) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
	return nil, errdefs.ErrNotImplemented

}

func (s *shimService) StartShim(ctx context.Context, opts shimv2.StartOpts) (string, error) {
	return "", errdefs.ErrNotImplemented
}



func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	return nil, errdefs.ErrNotImplemented

}

func (s *shimService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented

}

func (s *shimService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	return nil, errdefs.ErrNotImplemented

}

func (s *shimService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	return nil, errdefs.ErrNotImplemented

}

// func (s *shimService) StartShim()

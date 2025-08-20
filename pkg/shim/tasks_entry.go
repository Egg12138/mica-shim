package shim

import (
	"context"
	"fmt"
	log "mica-shim/logger"
	er "mica-shim/pkg/errors"
	utils "mica-shim/pkg/fileutils"
	cntr "mica-shim/pkg/micantainer"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	ptypes "github.com/containerd/containerd/protobuf/types"
)

// Create creates a new containerd task and **setup rtos Client**
// The init process is now a true init process :
// 1. satisfy containerd's requirements
// 2. as an agent, managing something needed in future(may be removed or not)
// TALK: the init process receives signals from containerd,
func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	log.Debugf("*** TASK CREATE: Request details - Bundle: %s, Stdin: %s, Stdout: %s, Stderr: %s, Terminal: %v",
		r.Bundle, r.Stdin, r.Stdout, r.Stderr, r.Terminal)

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := utils.ValidContainerID(r.ID); err != nil {
		return nil, er.ErrInvalidCID
	}

	type Result struct {
		container *cntr.Container
		err       error
	}

	ch := make(chan Result, 1)
	go func() {
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("create container instance timeout: %v", r.ID)
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		container := res.container
		container.SetStatus(task.Status_CREATED)
		// TODO: proc -> container
		// Create startup context - this will be signaled when the task is ready
		startupCtx, startupCancel := context.WithCancel(context.Background())
		// Create lifecycle context for actual task management
		lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
		// Create done context for proper task exit handling
		doneCtx, doneCancel := context.WithCancel(context.Background())

		s.procs[r.ID] = &initProcess{
			pid:             1,
			doneCtx:         doneCtx,
			doneCancel:      doneCancel,
			lifecycleCtx:    lifecycleCtx,
			lifecycleCancel: lifecycleCancel,
			startupCtx:      startupCtx,
			startupCancel:   startupCancel,
		}
		return &taskAPI.CreateTaskResponse{
			Pid: 1,
		}, nil
	}

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


func (s *shimService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}
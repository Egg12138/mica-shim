package shim

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	er "mica-shim/errors"
	log "mica-shim/logger"
	utils "mica-shim/pkg/utils"
	"os"
	"syscall"

	"github.com/containerd/containerd/api/events"
	"github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	ptypes "github.com/containerd/containerd/protobuf/types"
)

const (
	okExitCode = 0
	Exit255    = 255
)

var emptyResponse = &ptypes.Empty{}

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
		container *container
		err       error
	}

	ch := make(chan Result, 1)
	go func() {
		container, err := create(ctx, s, r)
		ch <- Result{container, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("create container instance timeout: %v", r.ID)
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		container := res.container
		container.status = task.Status_CREATED
		s.containers[r.ID] = container

		s.send(&events.TaskCreate{
			ContainerID: r.ID,
			Bundle:      r.Bundle,
			Rootfs:      r.Rootfs,
			IO: &events.TaskIO{
				Stdin:    r.Stdin,
				Stdout:   r.Stdout,
				Stderr:   r.Stderr,
				Terminal: r.Terminal,
			},
			Checkpoint: r.Checkpoint,
			// Pid is ExecID in comming task requests
			Pid: shimPid,
		})

		return &taskAPI.CreateTaskResponse{
			Pid: shimPid,
		}, nil
	}

}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Infof("shim Start() container %s", r.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		log.Debugf("container %s not found in shimservice storage", r.ID)
		return nil, er.ErrContainerNotFound
	}

	s.eventSendMu.Lock()
	defer s.eventSendMu.Unlock()

	// wannna start a exec process in a container
	if r.ExecID != "" {
		log.Debugf("container %s has no exec process", r.ID)
		s.send(&events.TaskExecStarted{
			ContainerID: c.id,
			ExecID:      r.ExecID,
			Pid:         shimPid,
		})
	} else {
		log.Info("starting container ")
		err := startContainer(ctx, s, c)
		if err != nil {
			return nil, errdefs.ToGRPC(err)
		}
		s.send(&events.TaskStart{
			ContainerID: c.id,
			Pid:         shimPid,
		})
	}

	return &taskAPI.StartResponse{
		Pid: shimPid,
	}, nil
}

// Delete alwasy delete container
func (s *shimService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Debugf("deleting container %s", r.ID)

	c, found := s.containers[r.ID]
	if c == nil || !found {
		log.Debugf("container %s not found in shimservice storage", r.ID)
		return &taskAPI.DeleteResponse{
			ExitStatus: okExitCode,
			ExitedAt:   timestamppb.Now(),
			Pid:        shimPid,
		}, nil
		// return nil, errdefs.ToGRPCf(errdefs.ErrNotFound, "container %s not found", r.ID)
	}

	if r.ExecID != "" {
		log.Debugf("container %s has no exec process", r.ID)
		return &taskAPI.DeleteResponse{
			ExitStatus: okExitCode,
			ExitedAt:   timestamppb.Now(),
			Pid:        shimPid,
		}, nil
	}

	if err := deleteContainer(ctx, s, c); err != nil {
		return nil, err
	}

	s.send(&events.TaskDelete{
		ContainerID: r.ID,
		ExitedAt:    timestamppb.New(c.exitTime),
		Pid:         shimPid,
		ExitStatus:  okExitCode,
	})

	return &taskAPI.DeleteResponse{
		ExitStatus: okExitCode,
		ExitedAt:   timestamppb.New(c.exitTime),
		Pid:        shimPid,
	}, nil
}

func (s *shimService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	log.Debugf("Pids() start")
	info := task.ProcessInfo{
		Pid: shimPid,
	}
	proc := make([]*task.ProcessInfo, 1)
	proc[0] = &info
	return &taskAPI.PidsResponse{
		Processes: proc,
	}, nil
}

func (s *shimService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if !found || c == nil {
		return nil, er.ErrContainerNotFound
	}
	c.status = task.Status_PAUSING
	err := s.sandbox.PauseContainer(ctx, r.ID)
	if err == nil {
		c.status = task.Status_PAUSED
		s.send(&events.TaskPaused{
			ContainerID: c.id,
		})
		return emptyResponse, nil
	}

	status, err := s.getContainerStatus(c.id)
	if err != nil {
		log.Debugf("failed to getcontaienr status, now status is %s, due to %v", status, err)
		c.status = task.Status_UNKNOWN
	} else {
		log.Debugf("succeffully getcontaienr status, now status is %s", status)
		c.status = status
	}

	return emptyResponse, nil

}

// NOTICE:mica differs from other isolation strategies (like namespace, cgroup, VM, etc.) - mica launches a client OS via pedestal
// to execute a highly secure application; this application's lifecycle is completely aligned with the client OS
// This OS is typically a realtime OS, where pause/resume operations are generally not desired, so we implement pause as stop
// and resume as client OS boot.
// Additionally, current mica client registration has some overhead, so we recommend users restart client OS via `ctr task resume`.
func (s *shimService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ErrContainerNotFound
	}

	err := s.sandbox.ResumeContainer(ctx, c.id)
	if err == nil {
		c.status = task.Status_RUNNING
		s.send(&events.TaskResumed{
			ContainerID: c.id,
		})
		return emptyResponse, nil
	}

	if status, err := s.getContainerStatus(c.id); err != nil {
		c.status = task.Status_UNKNOWN
	} else {
		c.status = status
	}

	return emptyResponse, err

}

// convert some POSIX signals into sandbox operations, and apply the operation to the task
func (s *shimService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// TODO: after mica supports passing POSIX signals to client os, we use sandbox.SignalTask to kill task
	signum := syscall.Signal(r.Signal)

	c, found := s.containers[r.ID]
	if !found {
		return nil, er.ErrContainerNotFound
	}

	// reject kill request for some exec process in a container due to micran 1:1:1 model
	// if r.All = true, we can pass the signal to the container
	if r.ExecID != "" && !r.All {
		log.Infof("container %s has no exec process %s", r.ID, r.ExecID)
		return emptyResponse, nil
	}

	switch signum {
	case syscall.SIGKILL, syscall.SIGTERM:
		if c.status == task.Status_STOPPED {
			log.Infof("container %s already stopped", c.id)
			return emptyResponse, nil
		}
		log.Debugf("in sandbox <%s>, tring to kill container %s", s.id, c.id)
		killed, err := s.sandbox.KillContainer(ctx, c.id)
		if err != nil {
			log.Pretty("kill container failed %v", err)
			st, err1 := s.getContainerStatus(c.id)
			if err1 != nil {
				log.Debugf("failed to get Container status: %v, mark status to UNKNOWN", err1)
				c.status = task.Status_UNKNOWN
			} else {
				c.status = st
			}
			return nil, err
		}
		c.status = task.Status_STOPPED
		log.Pretty("killed contaienr %v", killed.Status())
		return emptyResponse, nil
	case syscall.SIGSTOP, syscall.SIGCONT:
		if c.status == task.Status_PAUSING || c.status == task.Status_STOPPED {
			log.Infof("container %s pausing or stopped, can not task action", c.id)
			return emptyResponse, nil
		}
		if err := s.sandbox.PauseContainer(ctx, c.id); err != nil {
			log.Debugf("sandbox pause container %s failed %v", c.id, err)
			st, err1 := s.getContainerStatus(c.id)
			if err1 != nil {
				c.status = task.Status_UNKNOWN
			} else {
				c.status = st
			}
			return nil, err
		}
	default:
		return emptyResponse, nil
	}
	return emptyResponse, nil
}

// really passing signals to sandbox
func (s *shimService) KillBySignal(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// TODO: after mica supports passing POSIX signals to client os, we use sandbox.SignalTask to kill task
	signum := syscall.Signal(r.Signal)

	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ErrContainerNotFound
	}

	// Only supported
	if (signum == syscall.SIGKILL || signum == syscall.SIGTERM) && c.status == task.Status_STOPPED {
		log.Infof("container %s already stopped", c.id)
		return emptyResponse, nil
	}
	return emptyResponse, s.sandbox.SignalTask(ctx, c.id, signum)
}

func (s *shimService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	log.Warn("exec a new task is not supported")
	log.Pretty("exec request: %v", r)
	s.send(&events.TaskExecAdded{
		ContainerID: r.ID,
		ExecID:      r.ExecID,
	})
	return emptyResponse, nil
}

// NOTICE: always consider resizepty request is to contaienr, whatever r.ExecID is
func (s *shimService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	log.Debugf("resize pty: (%d, %d)", r.Height, r.Width)
	c, found := s.containers[r.ID]
	if !found || c == nil {
		return nil, er.ErrContainerNotFound
	}

	err := s.sandbox.WinResize(ctx, r.ID, r.Height, r.Width)
	if err != nil {
		return nil, err
	}
	return emptyResponse, nil
}

// closeIO closes the IO streams for a client os
func (s *shimService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ErrContainerNotFound
	}

	// TALK: if execid is not empty, should we close IO still?
	if r.ExecID != "" {
		return emptyResponse, nil
	}

	stdin := c.stdinPipe
	stdinCloser := c.stdinCloser

	<-stdinCloser
	if err := stdin.Close(); err != nil {
		log.Errorf("failed to close stdin pipe: %v", err)
		return nil, fmt.Errorf("failed to close stdin pipe: %w", err)
	}
	return emptyResponse, nil
}

func (s *shimService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "service Checkpoint")
}

// URGE: implement update task
func (s *shimService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ErrContainerNotFound
	}

	var res *specs.LinuxResources
	raw, err := typeurl.UnmarshalAny(r.Resources)
	if err != nil {
		return nil, err
	}

	res, found = raw.(*specs.LinuxResources)
	if !found {
		return nil, errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "Invalid resources type for %s", s.id)
	}
	log.Infof("update task annotations: %v", r.Annotations)
	log.Infof("update task resource: %v", res)
	err = s.sandbox.UpdateContainer(ctx, r.ID, *res)
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}
	return emptyResponse, nil
}

func (s *shimService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	s.mu.Lock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		s.mu.Unlock()
		return nil, er.ErrContainerNotFound
	}
	// Capture current status and the exit channel, then release the lock while waiting
	exitCh := c.exitCh
	exited := c.status == task.Status_STOPPED
	exitStatus := c.exit
	exitAt := c.exitTime
	s.mu.Unlock()

	// If not already exited, wait for exit or context cancellation
	if !exited {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait canceled: %w", ctx.Err())
		case <-exitCh:
			// Proceed to read updated exit status and time
		}
	}

	// Re-acquire lock to fetch final status/time
	s.mu.Lock()
	exitStatus = c.exit
	exitAt = c.exitTime
	s.mu.Unlock()

	return &taskAPI.WaitResponse{
		ExitStatus: exitStatus,
		ExitedAt:   timestamppb.New(exitAt),
	}, nil
}

func (s *shimService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &taskAPI.ConnectResponse{
		ShimPid: shimPid,
		TaskPid: shimPid,
	}, nil
}

// shutdown shimv2 but keep mica dameon active, when no containers in sandbox
// NOTICE: micran have no permission to manage the life cycle of mica daemon
// TALK:   in future, after micran embedded into mica, shutdown will close mica daemon when
//
//	there is no any sandbox in mica daemon scope
func (s *shimService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.containers) != 0 {
		s.mu.Unlock()
		return emptyResponse, nil
	}

	s.mu.Unlock()
	s.ss()

	// os.Exit() will terminate program immediately, the defer functions won't be executed,
	// so we add defer functions again before os.Exit().
	// Refer to https://pkg.go.dev/os#Exit
	os.Exit(0)

	// This will never be called, but this is only there to make sure the
	// program can compile.
	return emptyResponse, nil

}


func (s *shimService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ErrContainerNotFound
	}

	stats, err := marshalMetrics(ctx, s, r.ID)
	if err != nil {
		log.Debugf("failed to marshal stats: %v", err)
	}

	if defs.IsMock && err != nil {
		dummyStats, err := s.DummyStats()
		if err != nil {
			return &taskAPI.StatsResponse{Stats: nil}, nil
		}
		log.Debugf("returning dummy stats for container %s", r.ID)
		stats = dummyStats
	}

	return &taskAPI.StatsResponse{
		Stats: stats,
	}, nil
}

func (s *shimService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, fmt.Errorf("container %s not found", r.ID)
	}

	return &taskAPI.StateResponse{
		ID:         c.id,
		Bundle:     c.bundle,
		Pid:        shimPid,
		Status:     c.status,
		Stdin:      c.stdin,
		Stdout:     c.stdout,
		Stderr:     c.stderr,
		Terminal:   c.terminal,
		ExitStatus: c.exit,
		ExitedAt:   timestamppb.New(c.exitTime),
		ExecID:     r.ExecID,
	}, nil
}

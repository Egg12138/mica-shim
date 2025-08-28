package shim

import (
	"context"
	"fmt"
	log "mica-shim/logger"
	er "mica-shim/pkg/errors"
	utils "mica-shim/pkg/fileutils"
	"os"
	"syscall"

	"github.com/containerd/containerd/api/events"
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
			Pid: s.shimPid,
		})

		return &taskAPI.CreateTaskResponse{
			Pid: s.shimPid,
		}, nil
	}

}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Infof("shim Start() container %s", r.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.containers[r.ID]
	if c == nil || !ok {
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
			Pid:         s.shimPid,
		})
	} else {
		err := startContainer(ctx, s, c)
		if err != nil {
			return nil, errdefs.ToGRPC(err)
		}
		s.send(&events.TaskStart{
			ContainerID: c.id,
			Pid:         s.shimPid,
		})
	}

	return &taskAPI.StartResponse{
		Pid: s.shimPid,
	}, nil
}

// Delete alwasy delete container
func (s *shimService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.containers[r.ID]
	if c == nil || !ok {
		log.Debugf("container %s not found in shimservice storage")
		return nil, errdefs.ToGRPCf(errdefs.ErrNotFound, "container %s not found", r.ID)
	}

	if r.ExecID != "" {
		log.Debugf("container %s has no exec process", r.ID)
		return &taskAPI.DeleteResponse{
			ExitStatus: okExitCode,
			ExitedAt:   timestamppb.Now(),
			Pid:        s.shimPid,
		}, nil
	}

	if err := deleteContainer(ctx, s, c); err != nil {
		return nil, err
	}

	s.send(&events.TaskDelete{
		ContainerID: r.ID,
		ExitedAt:    timestamppb.New(c.exitTime),
		Pid:         s.shimPid,
		ExitStatus:  c.exit,
	})

	return &taskAPI.DeleteResponse{
		ExitStatus: c.exit,
		ExitedAt:   timestamppb.New(c.exitTime),
		Pid:        s.shimPid,
	}, nil
}

func (s *shimService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	log.Debugf("Pids() start")
	info := task.ProcessInfo{
		Pid: s.shimPid,
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
	c, ok := s.containers[r.ID]
	if !ok || c == nil {
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

	if status, err := s.getContainerStatus(c.id); err != nil {
		c.status = task.Status_UNKNOWN
	} else {
		c.status = status
	}

	return emptyResponse, nil

}

func (s *shimService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.containers[r.ID]
	if c == nil || !ok {
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

	c, ok := s.containers[r.ID]
	if c == nil || !ok {
		return nil, er.ErrContainerNotFound
	}

	// reject kill request for some exec process in a container due to micran 1:1:1 model
	// if r.All = true, we can pass the signal to the container
	if r.ExecID != "" && r.All == false {
		log.Warnf("container %s has no exec process %s", r.ID, r.ExecID)
		return emptyResponse, nil
	}

	switch signum {
	case syscall.SIGKILL | syscall.SIGTERM:
		if c.status == task.Status_STOPPED {
			log.Infof("container %s already stopped", c.id)
			return emptyResponse, nil
		}
		log.Debugf("in sandbox <%s>, tring to kill container %s", s.id, c.id)
		container, err := s.sandbox.KillContainer(ctx, c.id)
		if err != nil {
			log.Pretty("kill container failed %v", container.State())
			return nil, err
		}
		return emptyResponse, nil
	case syscall.SIGSTOP | syscall.SIGCONT:
		if c.status == task.Status_PAUSING {
			log.Infof("container %s pausing, wait for pausing done")
			return emptyResponse, nil
		}
		if err := s.sandbox.PauseContainer(ctx, c.id); err != nil {
			log.Debugf("sandbox pause container %s failed %v", c.id, err)
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

	c, ok := s.containers[r.ID]
	if c == nil || !ok {
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
	c, ok := s.containers[r.ID]
	if !ok || c == nil {
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
	c, ok := s.containers[r.ID]
	if c == nil || !ok {
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

func (s *shimService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	log.Debugf("update task: %v", r.Annotations)
	log.Warnf("micran does not support updating container resources yet")
	return emptyResponse, nil
}

func (s *shimService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	s.mu.Lock()
	c, ok := s.containers[r.ID]
	if c == nil || !ok {
		return nil, er.ErrContainerNotFound
	}
	s.mu.Unlock()

	return nil, errdefs.ErrNotImplemented

}

func (s *shimService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &taskAPI.ConnectResponse{
		ShimPid: s.shimPid,
		TaskPid: s.shimPid,
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
	log.Warnf("protocol not implemented yet")
	return &taskAPI.StatsResponse{Stats: nil}, nil
}

func (s *shimService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.containers[r.ID]
	if c == nil || !ok {
		return nil, fmt.Errorf("container %s not found", r.ID)
	}

	return &taskAPI.StateResponse{
		ID:         c.id,
		Bundle:     c.bundle,
		Pid:        s.shimPid,
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

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
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

const (
	okExitCode = 0
	Exit255    = 255
)

var emptyResponse = &ptypes.Empty{}

// Create creates a new containerd task and sets up the RTOS client.
// The init process satisfies containerd's requirements and acts as an agent for future needs.
// TALK: The init process receives signals from containerd.
func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	log.Debugf("creating task %s (bundle: %s, terminal: %v)", r.ID, r.Bundle, r.Terminal)

	if err := utils.ValidContainerID(r.ID); err != nil {
		return nil, er.InvalidCID
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

		// lock when updating shared state
		s.mu.Lock()
		container.status = task.Status_CREATED
		s.containers[r.ID] = container
		s.mu.Unlock()

		pid := container.pid
		if pid == 0 {
			pid = shimPid
		}

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
			Pid: pid,
		})

		return &taskAPI.CreateTaskResponse{
			Pid: pid,
		}, nil
	}

}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Debugf("starting container %s", r.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		log.Debugf("container %s not found in shim service storage", r.ID)
		return nil, er.ContainerNotFound
	}

	s.eventSendMu.Lock()
	defer s.eventSendMu.Unlock()

	respPid := shimPid
	// wannna start a exec process in a container
	if r.ExecID != "" {
		execProc, exists := c.execs[r.ExecID]
		if !exists {
			return nil, errdefs.ToGRPCf(errdefs.ErrNotFound, "exec %s not found in container %s", r.ExecID, r.ID)
		}
		if c.status != task.Status_RUNNING {
			return nil, errdefs.ToGRPCf(errdefs.ErrFailedPrecondition, "container %s must be running to start exec %s", r.ID, r.ExecID)
		}
		execProc.markStarted(shimPid)
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
		pid := c.pid
		if pid == 0 {
			pid = shimPid
		}
		s.send(&events.TaskStart{
			ContainerID: c.id,
			Pid:         pid,
		})
		respPid = pid
	}

	return &taskAPI.StartResponse{
		Pid: respPid,
	}, nil
}

// Delete always deletes the container.
func (s *shimService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Debugf("deleting container %s", r.ID)

	c, found := s.containers[r.ID]
	if c == nil || !found {
		log.Debugf("container %s not found in shim service storage", r.ID)
		return &taskAPI.DeleteResponse{
			ExitStatus: okExitCode,
			ExitedAt:   timestamppb.Now(),
			Pid:        shimPid,
		}, nil
		// return nil, errdefs.ToGRPCf(errdefs.ErrNotFound, "container %s not found", r.ID)
	}

	if r.ExecID != "" {
		execProc, exists := c.execs[r.ExecID]
		if !exists {
			return &taskAPI.DeleteResponse{
				ExitStatus: okExitCode,
				ExitedAt:   timestamppb.Now(),
				Pid:        shimPid,
			}, nil
		}

		changed := execProc.markExited(okExitCode)
		exitStatus := execProc.exitStatus
		exitTime := execProc.exitTime
		pid := execProc.pid
		if pid == 0 {
			pid = shimPid
		}
		delete(c.execs, r.ExecID)

		if changed {
			s.send(&events.TaskExit{
				ContainerID: r.ID,
				ID:          r.ExecID,
				Pid:         pid,
				ExitStatus:  exitStatus,
				ExitedAt:    timestamppb.New(exitTime),
			})
		}

		return &taskAPI.DeleteResponse{
			ExitStatus: exitStatus,
			ExitedAt:   timestamppb.New(exitTime),
			Pid:        pid,
		}, nil
	}

	if err := deleteContainer(ctx, s, c); err != nil {
		return nil, err
	}

	pid := c.pid
	if pid == 0 {
		pid = shimPid
	}

	s.send(&events.TaskDelete{
		ContainerID: r.ID,
		ExitedAt:    timestamppb.New(c.exitTime),
		Pid:         pid,
		ExitStatus:  okExitCode,
	})

	return &taskAPI.DeleteResponse{
		ExitStatus: okExitCode,
		ExitedAt:   timestamppb.New(c.exitTime),
		Pid:        pid,
	}, nil
}

func (s *shimService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	log.Debugf("pids() start")
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
		return nil, er.ContainerNotFound
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
		log.Debugf("container %s status query failed: %v", r.ID, err)
		c.status = task.Status_UNKNOWN
	} else {
		log.Debugf("container %s status: %s", r.ID, status)
		c.status = status
	}

	return emptyResponse, nil

}

// NOTICE: Mica uses client OS via pedestal for isolation, with application lifecycle aligned to client OS.
// Pause/resume operations are implemented as stop/boot due to realtime OS constraints.
func (s *shimService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ContainerNotFound
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

// Kill converts POSIX signals into sandbox operations and applies them to the task.
func (s *shimService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// TODO: after mica supports passing POSIX signals to client os, we use sandbox.SignalTask to kill task
	signum := syscall.Signal(r.Signal)

	c, found := s.containers[r.ID]
	if !found {
		return nil, er.ContainerNotFound
	}

	// reject kill request for some exec process in a container due to micran 1:1:1 model
	// if r.All = true, we can pass the signal to the container
	if r.ExecID != "" && !r.All {
		log.Debugf("container %s has no exec process %s", r.ID, r.ExecID)
		return emptyResponse, nil
	}

	switch signum {
	case syscall.SIGKILL, syscall.SIGTERM:
		if c.status == task.Status_STOPPED {
			log.Debugf("container %s already stopped", c.id)
			return emptyResponse, nil
		}
		log.Debugf("in sandbox <%s>, tring to kill container %s", s.id, c.id)
		killed, err := s.sandbox.KillContainer(ctx, c.id)
		if err != nil {
			st, err1 := s.getContainerStatus(c.id)
			log.Debugf("kill container %s failed: %v", c.id, err)
			if err1 != nil {
				log.Debugf("container %s status query failed during kill: %v", c.id, err1)
				c.status = task.Status_UNKNOWN
			} else {
				c.status = st
			}
			return nil, err
		}
		c.status = task.Status_UNKNOWN
		c.signalExit()
		log.Pretty("killed contaienr %v", killed.Status())
		return emptyResponse, nil
	case syscall.SIGSTOP, syscall.SIGCONT:
		if c.status == task.Status_PAUSING || c.status == task.Status_STOPPED {
			log.Debugf("container %s pausing or stopped, can not task action", c.id)
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

// KillBySignal passes signals directly to the sandbox.
func (s *shimService) KillBySignal(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// TODO: after mica supports passing POSIX signals to client os, we use sandbox.SignalTask to kill task
	signum := syscall.Signal(r.Signal)

	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ContainerNotFound
	}

	// Only supported
	if (signum == syscall.SIGKILL || signum == syscall.SIGTERM) && c.status == task.Status_STOPPED {
		log.Debugf("container %s already stopped", c.id)
		return emptyResponse, nil
	}
	return emptyResponse, s.sandbox.SignalTask(ctx, c.id, signum)
}

// TODO: Pass command line string to pty
func (s *shimService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	if r.ExecID == "" {
		return nil, errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "missing exec id for container %s", r.ID)
	}

	s.mu.Lock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		s.mu.Unlock()
		return nil, er.ContainerNotFound
	}
	if _, exists := c.execs[r.ExecID]; exists {
		s.mu.Unlock()
		return nil, errdefs.ToGRPCf(errdefs.ErrAlreadyExists, "exec %s already exists in container %s", r.ExecID, r.ID)
	}
	c.execs[r.ExecID] = newExecProcess(r.ExecID)
	s.mu.Unlock()

	log.Debugf("registered exec %s for container %s", r.ExecID, r.ID)
	s.send(&events.TaskExecAdded{
		ContainerID: r.ID,
		ExecID:      r.ExecID,
	})
	return emptyResponse, nil
}

// NOTICE: Always consider resizepty request is to container, whatever r.ExecID is.
func (s *shimService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	log.Debugf("resizing PTY for container %s to %dx%d", r.ID, r.Width, r.Height)
	c, found := s.containers[r.ID]
	if !found || c == nil {
		return nil, er.ContainerNotFound
	}

	err := s.sandbox.WinResize(ctx, r.ID, r.Height, r.Width)
	if err != nil {
		return nil, err
	}
	return emptyResponse, nil
}

// CloseIO closes the IO streams for a client OS.
func (s *shimService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ContainerNotFound
	}

	// Exec IO shares the container PTY; mark exec completion when IO is closed.
	if r.ExecID != "" {
		if execProc, exists := c.execs[r.ExecID]; exists {
			changed := execProc.markExited(okExitCode)
			pid := execProc.pid
			if pid == 0 {
				pid = shimPid
			}
			exitStatus := execProc.exitStatus
			exitTime := execProc.exitTime
			if changed {
				s.send(&events.TaskExit{
					ContainerID: r.ID,
					ID:          r.ExecID,
					Pid:         pid,
					ExitStatus:  exitStatus,
					ExitedAt:    timestamppb.New(exitTime),
				})
			}
		}
		return emptyResponse, nil
	}

	if !r.Stdin {
		return emptyResponse, nil
	}

	stdinCloser := c.stdinCloser

	if c.ttyio != nil && c.ttyio.io != nil && c.ttyio.io.Stdin() != nil {
		if err := c.ttyio.io.Stdin().Close(); err != nil {
			log.Debugf("failed to drain containerd stdin reader for %s: %v", r.ID, err)
		}
	}

	<-stdinCloser

	if c.stdinPipe != nil {
		if err := c.stdinPipe.Close(); err != nil {
			log.Debugf("stdin pipe close for %s returned: %v", r.ID, err)
		}
	}

	return emptyResponse, nil
}

func (s *shimService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ToGRPCf(errdefs.ErrNotImplemented, "service Checkpoint")
}

// URGE: Implement update task.
func (s *shimService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.containers[r.ID]
	if c == nil || !found {
		return nil, er.ContainerNotFound
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
	log.Debugf("update task annotations: %v", r.Annotations)
	log.Debugf("update task resource: %v", res)
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
		return nil, er.ContainerNotFound
	}

	if r.ExecID != "" {
		execProc, exists := c.execs[r.ExecID]
		if !exists {
			s.mu.Unlock()
			return nil, errdefs.ToGRPCf(errdefs.ErrNotFound, "exec %s not found in container %s", r.ExecID, r.ID)
		}
		waitCh := execProc.waitCh
		exited := execProc.status == task.Status_STOPPED
		exitStatus := execProc.exitStatus
		exitAt := execProc.exitTime
		s.mu.Unlock()

		if !exited {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("wait canceled: %w", ctx.Err())
			case <-waitCh:
				exitStatus = execProc.exitStatus
				exitAt = execProc.exitTime
			}
		}

		return &taskAPI.WaitResponse{
			ExitStatus: exitStatus,
			ExitedAt:   timestamppb.New(exitAt),
		}, nil
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

// Shutdown shuts down shimv2 but keeps mica daemon active when no containers are in sandbox.
// NOTICE: Micran has no permission to manage the lifecycle of mica daemon.
// TALK: In future, after micran embedded into mica, shutdown will close mica daemon when there is no sandbox in mica daemon scope.
func (s *shimService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	s.mu.Lock()
	if len(s.containers) != 0 {
		s.mu.Unlock()
		return emptyResponse, nil
	}

	s.mu.Unlock()
	s.ss()

	// Clean up the shim socket before exiting to prevent "address already in use" errors
	// when the shim is restarted. The socket address is stored in the "address" file.
	if sockAddr, err := shimv2.ReadAddress("address"); err == nil && sockAddr != "" {
		log.Debugf("cleaning up shim socket: %s", sockAddr)
		if err := shimv2.RemoveSocket(sockAddr); err != nil {
			log.Warnf("failed to remove shim socket %s: %v", sockAddr, err)
		}
	} else if err != nil {
		log.Debugf("failed to read socket address file: %v", err)
	}

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
		return nil, er.ContainerNotFound
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

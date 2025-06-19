//go:build linux

/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package core

import (
	"context"
	"fmt"
	"mica-shim/io"
	"mica-shim/libmica"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	log "mica-shim/logger"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	tasktypes "github.com/containerd/containerd/api/types/task"

	"github.com/containerd/containerd/api/services/ttrpc/events/v1"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/protobuf"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"github.com/containerd/containerd/runtime/v2/shim"
)

var (
	_ shim.TTRPCService  = (*micaTaskService)(nil)
	_ taskAPI.TaskService = (*micaTaskService)(nil)
)

// func New(ctx context.Context, id string, publisher shim.Publisher, shutdown func()) (shim.Shim, error) {
// 	return &MicaService{}, nil
// }

type MicaService struct {
	mu								sync.Mutex
	cs 								map[string]*MicaContainer
	event 						chan *events.Envelope
}

type MicaContainer struct {
	ID          string
	Bundle      string
	Pid         uint32
	Status      uint8
	ExitStatus  uint32
	Stdin       string
	Stdout      string
	Stderr      string
	Terminal    bool
	Checkpoint  string
	m          	sync.RWMutex
}

// Containerd shim functions with command:
// Create a new container
// func (s *MicaService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (_ *taskAPI.CreateTaskResponse, err error) {

// StartShim is a binary call that executes a new shim returning the address
// func (s *MicaService) StartShim(ctx context.Context, opts shim.StartOpts) (string, error) {

// Cleanup is a binary call that cleans up any resources used by the shim when the service crashes
// func (s *MicaService) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {


// Start the primary user process inside the container
// func (s *MicaService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {

// Delete a process or container
// func (s *MicaService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {

// Exec an additional process inside the container
// func (s *MicaService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {

// ResizePty of a process
// func (s *MicaService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {

// State returns runtime state of a process
// func (s *MicaService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {

// Pause the container
// func (s *MicaService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {

// Resume the container
// func (s *MicaService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {

// Kill a process
// func (s *MicaService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {

// Pids returns all pids inside the container
// func (s *MicaService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {

// CloseIO of a process
// func (s *MicaService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {

// Checkpoint the container
// func (s *MicaService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {

// Connect returns shim information of the underlying service
// func (s *MicaService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {

// Shutdown is called after the underlying resources of the shim are cleaned up and the service can be stopped
// func (s *MicaService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {

// Stats returns container level system stats for a container and its processes
// func (s *MicaService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {

// Update the live container
// func (s *MicaService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {

// Wait for a process to exit
// func (s *MicaService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {

// Create creates a new task and runs its init process.
func (s *micaTaskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (_ *taskAPI.CreateTaskResponse, retErr error) {
	log.Debugf("create id:%s", r.ID)

	s.m.Lock()
	defer s.m.Unlock()
	if _, ok := s.procs[r.ID]; ok {
		return nil, errdefs.ErrAlreadyExists
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting current working directory: %w", err)
	}

	cmd := exec.CommandContext(ctx, "sh", "-c",
		"while date --rfc-3339=seconds; do "+
			"sleep 5; "+
			"done",
	)

	pio, err := io.NewPipeIO(r.Stdout)
	if err != nil {
		return nil, fmt.Errorf("creating pipe io for stdout %s: %w", r.Stdout, err)
	}

	go func() {
		if err := pio.Copy(ctx); err != nil {
			log.Warn("failed to copy from stdout pipe")
		}
	}()

	cmd.Stdout = pio.Writer()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("running init command: %w", err)
	}

	defer func() {
		if retErr != nil {
			if err := pio.Close(); err != nil {
				log.Error("failed to close stdout pipe io")
			}
			if err := cmd.Cancel(); err != nil {
				log.Error("failed to cancel task init command")
			}
		}
	}()

	pid := cmd.Process.Pid

	doneCtx, markDone := context.WithCancel(context.Background())

	go func() {
		defer markDone()

		if err := cmd.Wait(); err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				log.Errorf("failed to wait for init process %d", pid)
			}
		}

		if err := pio.Close(); err != nil {
			log.Error("failed to close stdout pipe io")
		}

		exitStatus := 255

		if cmd.ProcessState != nil {
			switch unixWaitStatus := cmd.ProcessState.Sys().(syscall.WaitStatus); {
			case cmd.ProcessState.Exited():
				exitStatus = cmd.ProcessState.ExitCode()
			case unixWaitStatus.Signaled():
				exitStatus = exitCodeSignal + int(unixWaitStatus.Signal())
			}
		} else {
			log.Warn("init process wait returned without setting process state")
		}

		s.m.Lock()
		defer s.m.Unlock()

		proc, ok := s.procs[r.ID]
		if !ok {
			log.Errorf("failed to write final status of done init process: task was removed")
		}

		proc.exitTime = time.Now()
		proc.exitStatus = exitStatus
	}()

	// If containerd needs to resort to calling the shim's "delete" command to
	// clean things up, having the process' pid readable from a file is the
	// only way for it to know what init process is associated with the task.
	pidPath := filepath.Join(filepath.Join(filepath.Dir(cwd), r.ID), initPidFile)
	if err := shim.WritePidFile(pidPath, pid); err != nil {
		return nil, fmt.Errorf("writing pid file of init process: %w", err)
	}

	s.procs[r.ID] = &initProcess{
		pid:     pid,
		doneCtx: doneCtx,
		stdout:  r.Stdout,
	}

	return &taskAPI.CreateTaskResponse{
		Pid: uint32(pid),
	}, nil
}

// Start starts the primary user process inside the task.
func (s *micaTaskService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Debugf("start id:%s execid:%s", r.ID, r.ExecID)

	// we do not support starting a previously stopped task, and the init
	// process was already started inside the Create RPC call, so we naively
	// return its stored PID
	s.m.RLock()
	defer s.m.RUnlock()
	proc, ok := s.procs[r.ID]
	libmica.MicaCreateMsg(r.ID)
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	return &taskAPI.StartResponse{
		Pid: uint32(proc.pid),
	}, nil
}

// Delete deletes a task.
func (s *micaTaskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	log.Debugf("delete id:%s execid:%s", r.ID, r.ExecID)

	s.m.Lock()
	defer s.m.Unlock()
	proc, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	if proc.exitTime.IsZero() {
		return nil, errdefs.ToGRPCf(errdefs.ErrFailedPrecondition, "init process %d is not done yet", proc.pid)
	}

	delete(s.procs, r.ID)

	return &taskAPI.DeleteResponse{
		Pid:        uint32(proc.pid),
		ExitStatus: uint32(proc.exitStatus),
		ExitedAt:   protobuf.ToTimestamp(proc.exitTime),
	}, nil
}

// Exec executes an additional process inside the task.
func (*micaTaskService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	log.Debugf("exec id:%s execid:%s", r.ID, r.ExecID)
	return nil, errdefs.ErrNotImplemented
}

// ResizePty resizes the pty of a process.
func (*micaTaskService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	log.Debugf("resizepty id:%s execid:%s", r.ID, r.ExecID)
	return nil, errdefs.ErrNotImplemented
}

// State returns the runtime state of a process.
func (s *micaTaskService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	log.Debugf("state id:%s execid:%s", r.ID, r.ExecID)

	s.m.RLock()
	defer s.m.RUnlock()
	proc, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	status := tasktypes.Status_RUNNING
	if !proc.exitTime.IsZero() {
		status = tasktypes.Status_STOPPED
	}

	return &taskAPI.StateResponse{
		ID:         r.ID,
		Pid:        uint32(proc.pid),
		Status:     status,
		Stdout:     proc.stdout,
		ExitStatus: uint32(proc.exitStatus),
		ExitedAt:   protobuf.ToTimestamp(proc.exitTime),
	}, nil
}

// Pause pauses a task.
func (*micaTaskService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	log.Debugf("pause id:%s", r.ID)
	return nil, errdefs.ErrNotImplemented
}

// Resume resumes a task.
func (*micaTaskService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	log.Debugf("resume id:%s", r.ID)
	return nil, errdefs.ErrNotImplemented
}

// Kill kills a process.
func (s *micaTaskService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	log.Debugf("kill id:%s execid:%s", r.ID, r.ExecID)

	s.m.RLock()
	defer s.m.RUnlock()
	proc, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	if proc.pid > 0 {
		p, _ := os.FindProcess(proc.pid)
		// The POSIX standard specifies that a null-signal can be sent to check
		// whether a PID is valid.
		if err := p.Signal(syscall.Signal(0)); err == nil {
			sig := syscall.Signal(r.Signal)
			if err := p.Signal(sig); err != nil {
				return nil, fmt.Errorf("sending %s to init process: %w", sig, err)
			}
		}
	}

	return &ptypes.Empty{}, nil
}

// Pids returns all pids inside a task.
func (s *micaTaskService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	log.Debugf("pids id:%s", r.ID)
	return nil, errdefs.ErrNotImplemented
}

// CloseIO closes the I/O of a process.
func (*micaTaskService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	log.Debugf("closeio id:%s execid:%s", r.ID, r.ExecID)
	return nil, errdefs.ErrNotImplemented
}

// Checkpoint creates a checkpoint of a task.
func (*micaTaskService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	log.Debugf("checkpoint id:%s", r.ID)
	return nil, errdefs.ErrNotImplemented
}

// Connect returns the shim information of the underlying service.
func (s *micaTaskService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	log.Debugf("connect id:%s", r.ID)

	s.m.RLock()
	defer s.m.RUnlock()
	proc, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	return &taskAPI.ConnectResponse{
		ShimPid: uint32(os.Getpid()),
		TaskPid: uint32(proc.pid),
	}, nil
}

// Shutdown is called after the underlying resources of the shim are cleaned up and the service can be stopped.
func (s *micaTaskService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	log.Debugf("shutdown id:%s", r.ID)

	s.ss.Shutdown()
	return &ptypes.Empty{}, nil
}

// Stats returns container level system stats for a task and its processes.
func (*micaTaskService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	log.Debugf("stats id:%s", r.ID)
	return nil, errdefs.ErrNotImplemented
}

// Update updates the live task.
func (*micaTaskService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	log.Debugf("update id:%s", r.ID)
	return nil, errdefs.ErrNotImplemented
}

// Wait waits for a process to exit while attached to a task.
func (s *micaTaskService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	log.Debugf("wait id:%s execid:%s", r.ID, r.ExecID)

	doneCtx, err := func() (context.Context, error) {
		s.m.RLock()
		defer s.m.RUnlock()
		proc, ok := s.procs[r.ID]
		if !ok {
			return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
		}
		return proc.doneCtx, nil
	}()
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-doneCtx.Done():
	}

	s.m.RLock()
	defer s.m.RUnlock()
	proc, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task was removed: %w", errdefs.ErrNotFound)
	}

	return &taskAPI.WaitResponse{
		ExitStatus: uint32(proc.exitStatus),
		ExitedAt:   protobuf.ToTimestamp(proc.exitTime),
	}, nil
}


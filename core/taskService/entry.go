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
	"mica-shim/libmica"
	"os"
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
)

type MicaService struct {
	mu    sync.Mutex
	cs    map[string]*MicaContainer
	event chan *events.Envelope
}

type MicaContainer struct {
	ID         string
	Bundle     string
	Pid        uint32
	Status     uint8
	ExitStatus uint32
	Stdin      string
	Stdout     string
	Stderr     string
	Terminal   bool
	Checkpoint string
	m          sync.RWMutex
}

// Start the client rtos with its entrypoint task and managing agent process
func (s *micaTaskService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Debugf("start id:%s execid:%s", r.ID, r.ExecID)

	s.m.RLock()
	defer s.m.RUnlock()

	proc, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	// Step 1: Start the RTOS client via micad
	// This will trigger PTY service creation in micad
	log.Infof("Starting RTOS client for task %s", r.ID)
	response, err := libmica.MicaCtl(libmica.MStart, r.ID)
	log.Debugf("start id:%s execid:%s response:%s; mica error: %v", r.ID, r.ExecID, response, err)

	if err != nil {
		return nil, fmt.Errorf("failed to start mica client: %w", err)
	}

	// Step 2: Wait a moment for micad to complete service registration and PTY creation
	// This is necessary because micad creates PTY devices asynchronously after client start
	log.Debugf("Waiting for micad to complete service initialization for task %s", r.ID)
	time.Sleep(2 * time.Second) // Give micad time to create PTY services

	// Step 3: Start MicaIO to handle PTY communication
	// This will discover and connect to the PTY device created by micad
	if proc.micaIO != nil {
		log.Debugf("Starting MicaIO for task %s", r.ID)
		if err := proc.micaIO.Start(); err != nil {
			log.Errorf("failed to start MicaIO for task %s: %v", r.ID, err)
			// Don't fail the start operation completely - the RTOS client is still running
			// We'll continue without PTY forwarding for now
			log.Warnf("Task %s started but PTY forwarding is not available", r.ID)
		} else {
			log.Infof("MicaIO started successfully for task %s, PTY device: %s", r.ID, proc.micaIO.GetPTYDevice())
		}
	}

	log.Infof("Successfully started MICA task %s", r.ID)
	return &taskAPI.StartResponse{
		Pid: uint32(proc.pid),
	}, nil
}

// Delete deletes a task.
func (s *micaTaskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	log.Debugf("delete id:%s execid:%s", r.ID, r.ExecID)

	s.m.Lock()
	defer s.m.Unlock()

	client, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	if client.exitTime.IsZero() {
		return nil, errdefs.ToGRPCf(errdefs.ErrFailedPrecondition, "init process %d is not done yet", client.pid)
	}

	// Clean up MicaIO resources
	if client.micaIO != nil {
		if err := client.micaIO.Close(); err != nil {
			log.Errorf("failed to close MicaIO for task %s: %v", r.ID, err)
		}
	}

	// NOTICE: remove first, then stop the client rtos
	response, err := libmica.MicaCtl(libmica.MRemove, r.ID)
	log.Debugf("delete id:%s execid:%s response:%s; mica error: %v", r.ID, r.ExecID, response, err)

	delete(s.procs, r.ID)

	return &taskAPI.DeleteResponse{
		Pid:        uint32(client.pid),
		ExitStatus: uint32(client.exitStatus),
		ExitedAt:   protobuf.ToTimestamp(client.exitTime),
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

// State returns the runtime state of a RTOS task process.
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

// NOTICE: mica does not provide pause/resume feature
func (*micaTaskService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	log.Debugf("pause id:%s", r.ID)
	return nil, errdefs.ErrNotImplemented
}

// NOTICE: mica does not provide pause/resume feature
func (*micaTaskService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	log.Debugf("resume id:%s", r.ID)
	return nil, errdefs.ErrNotImplemented
}

// Kill kills a process.
func (s *micaTaskService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	log.Debugf("kill id:%s execid:%s", r.ID, r.ExecID)

	s.m.RLock()
	defer s.m.RUnlock()

	response, err := libmica.MicaCtl(libmica.MStop, r.ID)
	log.Debugf("kill id:%s execid:%s response:%s; mica error: %v", r.ID, r.ExecID, response, err)

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

// TODO: currently it is just for Linux process
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
		// TODO: task pid is the placeholder process pid,
		TaskPid: uint32(proc.pid),
	}, nil
}

// Shutdown is called after the underlying resources of the shim are cleaned up and the service can be stopped.
func (s *micaTaskService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	log.Debugf("shutdown id:%s", r.ID)

	s.ss.Shutdown()
	return &ptypes.Empty{}, nil
}

// Stats returns **container level** system stats for a task and its processes.
func (*micaTaskService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	log.Debugf("stats id:%s", r.ID)
	_, err := libmica.MicaCtl(libmica.MStatus, r.ID)
	if err != nil {
		return nil, err
	}
	// micaStatus2TaskStats(response)
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

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

package entry

import (
	"context"
	"fmt"
	"mica-shim/pkg/fileutils"
	"mica-shim/pkg/libmica"
	"os"
	"sync"
	"syscall"
	"time"

	log "mica-shim/logger"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	tasktypes "github.com/containerd/containerd/api/types/task"

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/protobuf"
	ptypes "github.com/containerd/containerd/protobuf/types"
)

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

type initProcess struct {
	pid           int
	exitTime      time.Time
	exitStatus    int
	stdout        string
	stderr        string
	doneCtx       context.Context
	doneCancel    context.CancelFunc
	micaIO        *libmica.MicaIO
	lifecycleCtx  context.Context
	lifecycleCancel context.CancelFunc
	monitorCancel context.CancelFunc
}

// Delete deletes a task.
func (s *micaTaskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {

	s.m.Lock()
	defer s.m.Unlock()

	client, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	if client.exitTime.IsZero() {
		return nil, errdefs.ToGRPCf(errdefs.ErrFailedPrecondition, "init process %d is not done yet", client.pid)
	}

	// Cancel lifecycle context to stop any pending operations
	if client.lifecycleCancel != nil {
		client.lifecycleCancel()
	}

	// Cancel monitoring context
	if client.monitorCancel != nil {
		client.monitorCancel()
	}

	// Clean up MicaIO resources
	if client.micaIO != nil {
		if err := client.micaIO.Close(); err != nil {
			log.Errorf("failed to close MicaIO for task %s: %v", r.ID, err)
		}
	}

	if !fileutils.ClientExist(r.ID) {
		log.Debugf("delete id:%s execid:%s - client does not exist, assuming already removed", r.ID, r.ExecID)
	} else {
		// NOTICE: remove in runtime first, then stop the client rtos
		response, err := libmica.MicaCtl(libmica.MRemove, r.ID)
		log.Debugf("delete id:%s execid:%s response:%s; mica error: %v", r.ID, r.ExecID, response, err)

		if err != nil {
			log.Errorf("delete id:%s execid:%s - failed to send remove command to micad: %v", r.ID, r.ExecID, err)
			return nil, fmt.Errorf("failed to remove task: %w", err)
		}

		if !success(response) {
			log.Errorf("delete id:%s execid:%s - micad returned failure: %s", r.ID, r.ExecID, response)
			return nil, fmt.Errorf("micad failed to remove task: %s", response)
		}

		log.Debugf("delete id:%s execid:%s - successfully removed task from micad", r.ID, r.ExecID)
	}

	// delete(s.procs, r.ID)
	if err := fileutils.RemoveExternalStatFile(r.ID); err != nil {
		log.Errorf("failed to remove external state file: %v", err)
	}

	return &taskAPI.DeleteResponse{
		Pid:        uint32(client.pid),
		ExitStatus: uint32(client.exitStatus),
		ExitedAt:   protobuf.ToTimestamp(client.exitTime),
	}, nil
}

// Exec executes an additional process inside the task.
func (*micaTaskService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	log.Infof("Executing a new task is not implemented yet")
	return nil, nil
}

// ResizePty resizes the pty of a process.
func (*micaTaskService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	log.Infof("resizepty is not implemented yet")
	return nil, nil
}

// State returns the runtime state of a RTOS task process.
func (s *micaTaskService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {

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
	return nil, errdefs.ErrNotImplemented
}

// NOTICE: mica does not provide pause/resume feature
func (*micaTaskService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

// Kill kills a process.
func (s *micaTaskService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {

	s.m.RLock()
	defer s.m.RUnlock()

	proc, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	if !proc.exitTime.IsZero() {
		log.Debugf("kill id:%s execid:%s - task already stopped", r.ID, r.ExecID)
		return &ptypes.Empty{}, nil
	}

	// Check if client exists before attempting to stop it
	if !fileutils.ClientExist(r.ID) {
		log.Debugf("kill id:%s execid:%s - client does not exist, assuming already stopped", r.ID, r.ExecID)
		// Mark the task as exited since it doesn't exist
		go func() {
			select {
			case <-time.After(100 * time.Millisecond):
				log.Debugf("goroutine in kill() - calling handleTaskExit for non-existent client id:%s", r.ID)
				s.handleTaskExit(r.ID, int(r.Signal))
			case <-proc.lifecycleCtx.Done():
				log.Debugf("kill delayed exit canceled for non-existent task %s", r.ID)
			}
		}()
		return &ptypes.Empty{}, nil
	}

	response, err := libmica.MicaCtl(libmica.MStop, r.ID)

	if err != nil {
		log.Debugf("kill id:%s execid:%s response:%s; mica error: %v", r.ID, r.ExecID, response, err)
		return nil, fmt.Errorf("failed to stop task: %w", err)
	}

	if !success(response) {
		log.Errorf("kill id:%s execid:%s - micad returned failure: %s", r.ID, r.ExecID, response)
		return nil, fmt.Errorf("micad failed to stop task: %s", response)
	}

	log.Debugf("kill id:%s execid:%s - successfully sent stop command to micad", r.ID, r.ExecID)

	// Mark the task as exited when killed
	// In a real implementation, this would be done when micad notifies us of the actual exit
	// For now, we'll simulate this by calling handleTaskExit directly
	go func() {
		// Give micad a moment to process the stop command
		select {
		case <-time.After(100 * time.Millisecond):
			log.Debugf("goroutine in kill() - calling handleTaskExit for id:%s", r.ID)
			s.handleTaskExit(r.ID, int(r.Signal))
		case <-proc.lifecycleCtx.Done():
			log.Debugf("kill delayed exit canceled for task %s", r.ID)
		}
	}()

	if proc.pid > 0 {
		p, _ := os.FindProcess(proc.pid)
		// The POSIX standard specifies that a null-signal can be sent to check
		// whether a PID is valid.
		if err := p.Signal(syscall.Signal(0)); err == nil {
			sig := syscall.Signal(r.Signal)
			if err := p.Signal(sig); err != nil {
				log.Warnf("failed to send signal %s to init process %d: %v", sig, proc.pid, err)
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


func (s *micaTaskService) handleTaskExit(id string, exitCode int) {
	log.Debugf("handleTaskExit id:%s exitCode:%d", id, exitCode)
	s.m.Lock()
	log.Debugf("handleTaskExit fetched lock")
	defer s.m.Unlock()
	if proc, ok := s.procs[id]; ok {
		proc.exitTime = time.Now()
		proc.exitStatus = exitCode
		if proc.doneCancel != nil {
			proc.doneCancel()
		}
	}
}

// Wait waits for a process to exit while attached to a task.
func (s *micaTaskService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	log.Debugf("wait id:%s execid:%s", r.ID, r.ExecID)
	
	s.m.RLock()
	proc, ok := s.procs[r.ID]
	if !ok {
		s.m.RUnlock()
		return nil, fmt.Errorf("task was removed: %w", errdefs.ErrNotFound)
	}
	
	// Check if already exited
	if !proc.exitTime.IsZero() {
		s.m.RUnlock()
		return &taskAPI.WaitResponse{
			ExitStatus: uint32(proc.exitStatus),
			ExitedAt:   protobuf.ToTimestamp(proc.exitTime),
		}, nil
	}
	s.m.RUnlock()
	
	// Use the proc's doneCtx for waiting
	select {
	case <-proc.doneCtx.Done():
		log.Debugf("selected done")
		// Make sure we have the lock when accessing proc fields
		s.m.RLock()
		defer s.m.RUnlock()
		return &taskAPI.WaitResponse{
			ExitStatus: uint32(proc.exitStatus),
			ExitedAt:   protobuf.ToTimestamp(proc.exitTime),
		}, nil
	case <-ctx.Done():
		log.Debugf("ctx done(timeout)")
		return nil, ctx.Err()
	}
}

// taskAPI for shimService
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

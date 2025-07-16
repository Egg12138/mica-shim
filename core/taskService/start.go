package core

import (
	"context"
	"fmt"
	"mica-shim/libmica"
	log "mica-shim/logger"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/errdefs"
)

// Start the client rtos with its entrypoint task and managing agent process
func (s *micaTaskService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.LocateDebugf("start id:%s execid:%s", r.ID, r.ExecID)

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
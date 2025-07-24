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
	log.Debugf("*** TASK START: Starting task %s (execid: %s)", r.ID, r.ExecID)

	s.m.RLock()
	defer s.m.RUnlock()

	proc, ok := s.procs[r.ID]
	log.Debugf("r.ID: %s, s has %d procs", r.ID, len(s.procs))
	if !ok {
		log.Debugf("*** TASK START: Task %s not found in procs map", r.ID)
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}
	log.Debugf("*** TASK START: Found task %s in procs map, PID: %d", r.ID, proc.pid)

	// Step 1: Start the RTOS client via micad
	// This will trigger PTY service creation in micad
	log.Infof("Starting RTOS client for task %s", r.ID)
	log.Debugf("*** TASK START: About to start RTOS client for task %s via micad", r.ID)
	response, err := libmica.MicaCtl(libmica.MStart, r.ID)
	log.Debugf("start id:%s execid:%s response:%s; mica error: %v", r.ID, r.ExecID, response, err)
	log.Debugf("*** TASK START: MicaCtl response for task %s: %s, error: %v", r.ID, response, err)

	if err != nil {
		log.Debugf("*** TASK START: Failed to start mica client for task %s: %v", r.ID, err)
		return nil, fmt.Errorf("failed to start mica client: %w", err)
	}
	log.Debugf("*** TASK START: Successfully sent start command to micad for task %s", r.ID)

	// Step 2: Wait a moment for micad to complete service registration and PTY creation
	// This is necessary because micad creates PTY devices asynchronously after client start
	log.Debugf("Waiting for micad to complete service initialization for task %s", r.ID)
	log.Debugf("*** TASK START: Waiting 2 seconds for micad to create PTY services for task %s", r.ID)
	time.Sleep(2 * time.Second) // Give micad time to create PTY services
	fmt.Printf("A dummy execute output\n")
	log.Debugf("*** TASK START: Wait period completed for task %s", r.ID)

	// Step 3: Start MicaIO to handle PTY communication
	// This will discover and connect to the PTY device created by micad
	if proc.micaIO != nil {
		log.Debugf("*** TASK START: About to start MicaIO for task %s", r.ID)
		log.Debugf("*** TASK START: This is the critical path for Hello Zephyr output forwarding for task %s", r.ID)
		if err := proc.micaIO.Start(); err != nil {
			log.Errorf("failed to start MicaIO for task %s: %v", r.ID, err)
			log.Debugf("*** TASK START: MicaIO start failed for task %s: %v", r.ID, err)
			// Don't fail the start operation completely - the RTOS client is still running
			// We'll continue without PTY forwarding for now
			log.Warnf("Task %s started but PTY forwarding is not available", r.ID)
			log.Debugf("*** TASK START: Continuing without PTY forwarding for task %s", r.ID)
		} else {
			log.Infof("MicaIO started successfully for task %s, PTY device: %s", r.ID, proc.micaIO.GetPTYDevice())
			log.Debugf("*** TASK START: MicaIO successfully started for task %s, PTY: %s", r.ID, proc.micaIO.GetPTYDevice())
			log.Debugf("*** TASK START: Hello Zephyr output should now be forwarded through PTY %s for task %s", proc.micaIO.GetPTYDevice(), r.ID)
		}
	} else {
		log.Debugf("*** TASK START: No MicaIO instance found for task %s", r.ID)
	}

	log.Infof("Successfully started MICA task %s", r.ID)
	log.Debugf("*** TASK START: Task %s start process completed, returning PID %d", r.ID, proc.pid)
	return &taskAPI.StartResponse{
		Pid: uint32(proc.pid),
	}, nil
}

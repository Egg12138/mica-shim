package entry

import (
	"context"
	"fmt"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/container"
	"mica-shim/pkg/libmica"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/errdefs"
)

// Start the client rtos with its entrypoint task and managing agent process
func (s *micaTaskService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {

	s.m.RLock()
	defer s.m.RUnlock()

	proc, ok := s.procs[r.ID]
	if !ok {
		return nil, fmt.Errorf("task not created: %w", errdefs.ErrNotFound)
	}

	// Step 1: Start the RTOS client via micad
	// This will trigger PTY service creation in micad

	// BUG: A fatal error that start request do not pass bundle to shim, we can not
	// recover container state directly through bundle/state.json.
	stat, err := loadContainerState(r.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load container state: %w", err)
	}

	container, err := cntr.RestoreContainerFromState(stat)
	if err != nil {
		return nil, fmt.Errorf("failed to restore container from state: %w", err)
	}

	err = start(container, ctx, r)

	if err != nil {
		log.Debugf("*** TASK START: Failed to start mica client for task %s: %v", r.ID, err)
		return nil, fmt.Errorf("failed to start mica client: %w", err)
	}

	// Step 2: Wait a moment for micad to complete service registration and PTY creation
	// This is necessary because micad creates PTY devices asynchronously after client start
	log.Debugf("Waiting for micad to complete service initialization for task %s", r.ID)
	select {
	case <-time.After(500 * time.Millisecond):
		log.Debugf("*** TASK START: Wait period completed for task %s", r.ID)
	case <-ctx.Done():
		log.Debugf("*** TASK START: Context canceled while waiting for micad initialization for task %s", r.ID)
		return nil, ctx.Err()
	}

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

	// Start monitoring for task exit
	s.monitorTaskExit(r.ID)

	// For RTOS tasks, we need to signal that the task initialization is complete
	// This prevents containerd from getting stuck waiting for task readiness
	// We'll signal the startupCtx after a short delay to indicate the task is ready
	go func() {
		// Give a moment for everything to settle and PTY to be ready
		time.Sleep(300 * time.Millisecond)
		log.Debugf("*** TASK START: Task %s initialization complete, signaling readiness", r.ID)

		// Signal that the task has successfully started and is ready
		// This is important for containerd to know that the task is running
		s.m.Lock()
		if proc, exists := s.procs[r.ID]; exists && proc.startupCancel != nil {
			proc.startupCancel()
			log.Debugf("*** TASK START: Successfully signaled task readiness for %s", r.ID)
		}
		s.m.Unlock()
	}()

	return &taskAPI.StartResponse{
		Pid: uint32(proc.pid),
	}, nil
}

func start(container *cntr.Container, ctx context.Context, req *taskAPI.StartRequest) error {

	// log.Pretty("start container %s: %v", container.ID, container)
	conf, err := CreateMicaConf(container)
	if err != nil {
		return fmt.Errorf("failed to create mica client conf: %w", err)
	}
	res, err := libmica.MicaCreate(conf)
	if err != nil || !success(res) {
		return fmt.Errorf("failed to create mica client: %w", err)
	}
	log.Debugf("create mica client %s: %v", req.ID, res)

	res, err = libmica.MicaCtl(libmica.MStart, req.ID)
	if err != nil || !success(res) {
		return fmt.Errorf("failed to start mica client: %w", err)
	}
	log.Pretty("start mica client %s: %v", req.ID, res)

	return nil
}

// monitorTaskExit monitors the task status and calls handleTaskExit when the task exits
func (s *micaTaskService) monitorTaskExit(id string) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		// In a real implementation, this would listen for exit notifications from micad
		// For now, we'll just periodically check the task status
		ticker := time.NewTicker(10 * time.Second) // Check every 10 seconds
		defer ticker.Stop()

		s.m.RLock()
		proc, exists := s.procs[id]
		log.Debugf("monitor got lock")
		if !exists {
			s.m.RUnlock()
			log.Debugf("monitorTaskExit: task %s no longer exists", id)
			return
		}
		if !proc.exitTime.IsZero() {
			s.m.RUnlock()
			log.Debugf("monitorTaskExit: task %s already stopped", id)
			return
		}
		s.m.RUnlock()

		// Monitor for a reasonable time (e.g., 5 minutes)
		for {
			select {
			case <-ctx.Done():
				log.Debugf("monitorTaskExit: context canceled for task %s", id)
				return
			case <-ticker.C:
				// Check if task still exists and is running
				s.m.RLock()
				proc, exists := s.procs[id]
				if !exists {
					s.m.RUnlock()
					log.Debugf("monitorTaskExit: task %s no longer exists", id)
					return
				}
				if !proc.exitTime.IsZero() {
					s.m.RUnlock()
					log.Debugf("monitorTaskExit: task %s has been stopped", id)
					return
				}
				s.m.RUnlock()

				// In a real implementation, we would check with micad for task status
				// For now, we'll just continue monitoring
				log.Debugf("monitorTaskExit: task %s still running", id)
			}
		}
	}()

	// Store the cancel function so we can stop monitoring when task is deleted
	s.m.Lock()
	if proc, ok := s.procs[id]; ok {
		proc.monitorCancel = cancel
	}
	s.m.Unlock()
}

// 1. search bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TODO: Only copy values, the evaluation procedure is in the caller function
// TALK: mica does not support multi-core yet, so we only support single core for now.
func CreateMicaConf(container *cntr.Container) (libmica.MicaClientConf, error) {
	config := container.GetConfig()
	firmware := config.GetFirmwarePath()
	pedestal := config.GetPed()
	name := container.ID
	// TODO: Calculate the CPU lazily, we should calculate it in the container creation
	cpu, err := container.GetClientCPU()
	conf := libmica.MicaClientConf{}
	if err != nil {
		return conf, fmt.Errorf("failed to get client cpu: %w", err)
	}
	
	// Pass memory constraints for future use
	mem := uint64(config.MemoryLimit)
	conf.Init(uint32(cpu), mem, name, firmware, pedestal.PedestalType.String(), pedestal.PedestalConf, false)
	return conf, nil
}

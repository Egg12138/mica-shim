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
	container, err := loadContainerState(r.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load container state: %w", err)
	}

	err = start(container, ctx, r)

	if err != nil {
		log.Debugf("*** TASK START: Failed to start mica client for task %s: %v", r.ID, err)
		return nil, fmt.Errorf("failed to start mica client: %w", err)
	}


	// Step 2: Wait a moment for micad to complete service registration and PTY creation
	// This is necessary because micad creates PTY devices asynchronously after client start
	log.Debugf("Waiting for micad to complete service initialization for task %s", r.ID)
	time.Sleep(2 * time.Second) // Give micad time to create PTY services

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

func start(container *cntr.Container, ctx context.Context, req *taskAPI.StartRequest) error {

	log.Pretty("start container %s: %v", container.ID, container)
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

// 1. search bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TODO: Only copy values, the evaluation procedure is in the caller function
// TALK: mica does not support multi-core yet, so we only support single core for now.
func CreateMicaConf(container *cntr.Container) (libmica.MicaClientConf, error) {
	config := container.GetConfig()

	firmware := config.FirmwarePath()
	pedestal := config.Ped()
	name := container.ID
	// TODO: Calculate the CPU too late, we should calculate it in the container creation
	cpu, err := container.GetClientCPU()
	if err != nil {
		return libmica.MicaClientConf{}, fmt.Errorf("failed to get client cpu: %w", err)
	}
	conf := libmica.MicaClientConf{}
	log.Debugf("backed from GetClientCPU: %d", cpu)
	conf.Init(uint32(cpu), name, firmware, pedestal.PedestalType.String(), pedestal.PedestalConf, false)
	log.Pretty("MicaClientConf: %v", conf)
	return conf, nil
}

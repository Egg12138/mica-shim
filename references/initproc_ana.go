package references

import (
	"context"
	"fmt"
	"mica-shim/pkg/libmica"
	"time"

	"github.com/golang/protobuf/ptypes"
)

// Analysis: Do we need init process for RTOS management?

/*
TRADITIONAL LINUX CONTAINERS:
- Init process (PID 1) manages child processes
- Handles signals, zombie reaping, etc.
- Process tree: containerd -> shim -> init -> app processes

MICA RTOS CONTAINERS:
- RTOS runs on different CPU core via mica daemon
- No traditional process tree
- Process management handled by RTOS itself
- Communication via mica daemon, not process signals

CONCLUSION: You DON'T need traditional init process, but you DO need:
*/

// 1. Placeholder process for containerd API compatibility
type RTOSTaskManager struct {
	// Not a real init process, but satisfies containerd's PID requirements
	placeholderPID int    // Always return 1 or shim PID
	rtosClientID   string // Actual RTOS client identifier
	micaDaemon     *MicaDaemonClient
}

// 2. Simplified task management without init process
func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	// Don't create real process, just prepare RTOS configuration
	container, err := createContainer(r)
	if err != nil {
		return nil, err
	}

	// Store container info without init process
	s.m.Lock()
	s.containers[r.ID] = &TaskContainer{
		container: container,
		pid:       1, // Placeholder PID for containerd compatibility
		// No real process contexts needed
	}
	s.m.Unlock()

	return &taskAPI.CreateTaskResponse{Pid: 1}, nil
}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	// Start RTOS via mica daemon instead of process management
	container := s.containers[r.ID]

	// Create and start RTOS client
	conf, err := CreateMicaConf(container.container)
	if err != nil {
		return nil, err
	}

	// This replaces traditional process start
	if err := s.startRTOSClient(conf); err != nil {
		return nil, err
	}

	// Start monitoring RTOS status (not process status)
	go s.monitorRTOSClient(r.ID)

	return &taskAPI.StartResponse{Pid: 1}, nil
}

func (s *shimService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	// Signal RTOS termination via mica daemon instead of process kill
	response, err := libmica.MicaCtl(libmica.MStop, r.ID)
	if err != nil {
		return nil, err
	}

	// Simulate task exit after RTOS stops
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.handleRTOSExit(r.ID, int(r.Signal))
	}()

	return &ptypes.Empty{}, nil
}

// 3. RTOS-specific management functions
func (s *shimService) startRTOSClient(conf libmica.MicaClientConf) error {
	// Register and start RTOS client
	res, err := libmica.MicaCreate(conf)
	if err != nil || !success(res) {
		return fmt.Errorf("failed to create mica client: %w", err)
	}

	res, err = libmica.MicaCtl(libmica.MStart, conf.Name)
	if err != nil || !success(res) {
		return fmt.Errorf("failed to start mica client: %w", err)
	}

	return nil
}

func (s *shimService) monitorRTOSClient(id string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check RTOS status via mica daemon
			status, err := libmica.MicaCtl(libmica.MStatus, id)
			if err != nil || !success(status) {
				// RTOS has exited
				s.handleRTOSExit(id, 0)
				return
			}
		}
	}
}

func (s *shimService) handleRTOSExit(id string, exitCode int) {
	s.m.Lock()
	defer s.m.Unlock()

	if container, ok := s.containers[id]; ok {
		container.exitTime = time.Now()
		container.exitStatus = exitCode

		// Publish exit event for containerd
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		s.publishTaskExit(ctx, id, 1, uint32(exitCode), container.exitTime)
	}
}

/*
KEY INSIGHTS:
1. Use placeholder PID (1) for containerd API compatibility
2. Replace process management with RTOS client management via mica daemon
3. Replace signal handling with mica daemon communication
4. Still need proper event publishing for containerd
5. Monitoring becomes polling mica daemon status instead of process status
*/

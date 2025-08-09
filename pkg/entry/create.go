package entry

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/container"
	utils "mica-shim/pkg/fileutils"
	"os"
	"os/exec"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"

	"github.com/containerd/containerd/errdefs"
)

// Create creates a new containerd task and **setup rtos Client**
// The init process is now a true init process :
// 1. satisfy containerd's requirements
// 2. as an agent, managing something needed in future(may be removed or not)
// TALK: the init process receives signals from containerd,
func (s *micaTaskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (_ *taskAPI.CreateTaskResponse, retErr error) {
	log.Debugf("*** TASK CREATE: Request details - Bundle: %s, Stdin: %s, Stdout: %s, Stderr: %s, Terminal: %v",
		r.Bundle, r.Stdin, r.Stdout, r.Stderr, r.Terminal)

	s.m.Lock()
	defer s.m.Unlock()
	if _, ok := s.procs[r.ID]; ok {
		return nil, errdefs.ErrAlreadyExists
	}

	if err := utils.ValidContainerID(r.ID); err != nil {
		return nil, fmt.Errorf("invalid container id: %w", err)
	}
	// cwd, err := os.Getwd()
	// if err != nil {
	// 	log.Debugf("*** TASK CREATE: Failed to get working directory for task %s: %v", r.ID, err)
	// 	return nil, fmt.Errorf("getting current working directory: %w", err)
	// }
	// log.Debugf("*** TASK CREATE: Current working directory: %s", cwd)

	// Parse runtime configurations

	// Create mica client first - this registers with micad but doesn't start PTY services yet

	type Result struct {
		container *cntr.Container
		err       error
	}

	ch := make(chan Result, 1)
	go func() {
		container, err := createContainer(r)
		select {
		case ch <- Result{container, err}:
			// Result sent successfully
		case <-ctx.Done():
			// Context canceled, discard result to prevent goroutine leak
			log.Debugf("Create container for %s canceled, result discarded", r.ID)
		}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("create container instance timeout: %v", r.ID)
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		container := res.container
		container.SetStatus(task.Status_CREATED)
		// TODO: proc -> container
		// Create startup context - this will be signaled when the task is ready
		startupCtx, startupCancel := context.WithCancel(context.Background())
		// Create lifecycle context for actual task management
		lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
		// Create done context for proper task exit handling
		doneCtx, doneCancel := context.WithCancel(context.Background())

		s.procs[r.ID] = &initProcess{
			pid:             1,
			doneCtx:         doneCtx,
			doneCancel:      doneCancel,
			lifecycleCtx:    lifecycleCtx,
			lifecycleCancel: lifecycleCancel,
			startupCtx:      startupCtx,
			startupCancel:   startupCancel,
		}
		return &taskAPI.CreateTaskResponse{
			Pid: 1,
		}, nil
	}
}

// and mica client Agent process, helpful in CNI, container communications and so on
// <containerID>.sock -> <containerID>.agent
// But now, this is an dummy agent, it does nothing
func agent() *exec.Cmd {
	EchoAndSleepCommand := exec.CommandContext(context.Background(), "echo", "hello micran", "&&", "sleep", "1000")
	EchoAndSleepCommand.Stdout = os.Stdout
	EchoAndSleepCommand.Stderr = os.Stderr
	EchoAndSleepCommand.Stdin = os.Stdin

	// TODO: micad setup client socket, agent binds <containerID>.sock;
	return EchoAndSleepCommand
}

// setup the task and client os; without managing micataskservice
func createContainer(req *taskAPI.CreateTaskRequest) (c *cntr.Container, retErr error) {
	// parsed from bundle
	err := setupMicranStateDir()
	if err != nil {
		log.Debugf("failed to setup micran state directory: %w", err)
	}

	container, err := cntr.NewContainer(req.ID, req.Bundle, req.Rootfs, req.Terminal)
	if err != nil || container == nil {
		return nil, fmt.Errorf("failed to init Container %w", err)
	}

	if err = saveContainerState(container); err != nil {
		return nil, fmt.Errorf("failed to save container state: %w", err)
	}

	return container, err
}

// load from bundle first, then from micran state dir
func loadContainerState(id string) (*cntr.State, error) {
	cwd, err := os.Getwd()
	log.Debugf("cwd: %s", cwd)
	var st *cntr.State
	if err == nil {
		if st, err = cntr.LoadStateFromDir(cwd); err == nil {
			return st, nil
		}
	}

	stateDir := filepath.Join(defs.MicranStateDir, id)
	log.Debugf("load container state from %s: %v. try to load from micran state dir: %s", cwd, err, stateDir)

	st, err = cntr.LoadStateFromDir(stateDir)

	if err == nil && st != nil {
		if !utils.IdMatched(id, st.ID) {
			return nil, fmt.Errorf("container id mismatch: %s != %s", id, st.ID)
		}
		return st, nil
	}

	return nil, err
}

func setupMicranStateDir() error {
	if err := os.MkdirAll(defs.MicranStateDir, 0755); err != nil {
		return fmt.Errorf("failed to create micran state directory: %w", err)
	}
	return nil
}

func saveContainerState(c *cntr.Container) error {
	if err := os.MkdirAll(filepath.Join(defs.MicranStateDir, c.ID), 0o755); err != nil {
		log.Debugf("failed to create <%s> state directory: %v", c.ID)
		return err
	}
	return c.SaveState()
}

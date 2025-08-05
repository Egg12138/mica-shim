package entry

import (
	"context"
	"fmt"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/container"
	"mica-shim/pkg/libmica"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/runtime/v2/shim"

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

	// cwd, err := os.Getwd()
	// if err != nil {
	// 	log.Debugf("*** TASK CREATE: Failed to get working directory for task %s: %v", r.ID, err)
	// 	return nil, fmt.Errorf("getting current working directory: %w", err)
	// }
	// log.Debugf("*** TASK CREATE: Current working directory: %s", cwd)

	// Parse runtime configurations

	// Create mica client first - this registers with micad but doesn't start PTY services yet
	_, err := createContainer(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("creating mica client: %w", err)
	}

	// check micad 
	// cmd := agent()

	// // Initialize MicaIO for future PTY communication
	// // Note: PTY devices will be created later when mica client starts
	// log.Debugf("*** TASK CREATE: IO paths - stdin: %s, stdout: %s, stderr: %s", r.Stdin, r.Stdout, r.Stderr)
	micaIO, err := libmica.NewMicaIO(ctx, r.ID, r.Stdin, r.Stdout, r.Stderr, r.Terminal)
	if err != nil {
		return nil, fmt.Errorf("creating mica IO: %w", err)
	}
	log.Debugf("*** TASK CREATE: Successfully created MicaIO for task %s", r.ID)

	// // Start the MICA init process
	// // NOTICE: only if created successfully, handle pty
	// log.Debugf("*** TASK CREATE: Starting MICA init process for task %s", r.ID)


	// cmd.Start()
	// pid := cmd.Process.Pid
	// log.Debugf("Created MICA init process as agent, with PID %d for task %s", pid, r.ID)


	defer func() {
		if retErr != nil {
			if err := micaIO.Close(); err != nil {
				log.Debugf("Failed to close mica IO for %s: %v", r.ID, err)
			}
			// if cmd.Process != nil {
			// 	// Kill the MICA init process on error
			// 	if err := cmd.Process.Kill(); err != nil {
			// 		log.FDebugf("pid = %v, err = %v", cmd.Process.Pid, err)
			// 		log.Error("failed to kill MICA init process")
			// 		log.Debugf("*** TASK CREATE: Failed to kill MICA init process during cleanup for task %s: %v", r.ID, err)
			// 	}
			// }
		}
	}()


	doneCtx, markDone := context.WithCancel(context.Background())

	go func() {
		defer markDone()

		// if err := cmd.Wait(); err != nil {
		// 	if _, ok := err.(*exec.ExitError); !ok {
		// 		log.Errorf("failed to wait for MICA init process %d", pid)
		// 	}
		// }

		exitStatus := 255

		// if cmd.ProcessState != nil {
			// switch unixWaitStatus := cmd.ProcessState.Sys().(syscall.WaitStatus); {
			// case cmd.ProcessState.Exited():
			// 	exitStatus = cmd.ProcessState.ExitCode()
			// case unixWaitStatus.Signaled():
			// 	exitStatus = exitCodeSignal + int(unixWaitStatus.Signal())
			// }
		// } else {
		// 	log.Warn("MICA init process wait returned without setting process state")
		// }

		s.m.Lock()
		defer s.m.Unlock()

		proc, ok := s.procs[r.ID]
		if !ok {
			log.Errorf("failed to write final status of done MICA init process: task was removed")
			return
		}

		proc.exitTime = time.Now()
		proc.exitStatus = exitStatus

		// Close MicaIO when process exits
		if proc.micaIO != nil {
			if err := proc.micaIO.Close(); err != nil {
				log.Errorf("failed to close MicaIO on process exit: %v", err)
			}
		}
	}()

	// Write PID file for containerd compatibility
	cwd, err := os.Getwd()
	// TODO: for future agent process
	pid := 1
	if err != nil {
		log.Errorf("failed to get current working directory: %v", err)
		return nil, fmt.Errorf("getting current working directory: %w", err)
	}
	pidPath := filepath.Join(filepath.Join(filepath.Dir(cwd), r.ID), initPidFile)
	if err := shim.WritePidFile(pidPath, pid); err != nil {
		return nil, fmt.Errorf("writing pid file of MICA init process: %w", err)
	}

	s.procs[r.ID] = &initProcess{
		pid:     pid,
		doneCtx: doneCtx,
		stdout:  r.Stdout,
		micaIO:  micaIO,
	}

	log.Infof("Successfully created MICA task %s with init process PID %d", r.ID, pid)
	return &taskAPI.CreateTaskResponse{
		Pid: uint32(pid),
	}, nil
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
func createContainer(ctx context.Context, req *taskAPI.CreateTaskRequest) (taskRes *taskAPI.CreateTaskResponse, retErr error) {
	// parsed from bundle 
	err := setupMicranStateDir()
	if err != nil {
		log.Debugf("failed to setup micran state directory: %w", err)
	}

	container, err := cntr.NewContainer(req.ID, req.Bundle, req.Rootfs, req.Terminal)
	if err != nil || container == nil{
		return nil, fmt.Errorf("failed to init Container %w", err)
	}

	if err = saveContainerState(container); err != nil {
		return nil, fmt.Errorf("failed to save container state: %w", err)
	}

	taskRes = &taskAPI.CreateTaskResponse{
		Pid: 1,
	}
	// conf, err := CreateMicaConf(container)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create mica create conf: %w", err)
	// }

	// res, err := libmica.CreateMicaClient(conf)
	// if err != nil {
	// 	return nil, fmt.Errorf("mica create: %w", err)
	// }
	// if res == defs.MicaSuccess {
	// 	taskRes = &taskAPI.CreateTaskResponse{
	// 		Pid: 1,
	// 	}
	// 	log.Debugf("mica create success")
	// }

	return taskRes, nil
}


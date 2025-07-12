package core

import (
	"context"
	"fmt"
	"mica-shim/cntr"
	"mica-shim/libmica"
	log "mica-shim/logger"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/runtime/v2/shim"

	"github.com/containerd/containerd/errdefs"
)

// Create creates a new containerd task and **setup rtos Client**
// TODO: Currently, init process is just a placeholder process,
// we will complete it in future and make it a real good init process.
// TALK: the init process receives signals from containerd,
func (s *micaTaskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (_ *taskAPI.CreateTaskResponse, retErr error) {
	log.LocateDebugf("create id:%s", r.ID)
	s.m.Lock()
	defer s.m.Unlock()
	if _, ok := s.procs[r.ID]; ok {
		return nil, errdefs.ErrAlreadyExists
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting current working directory: %w", err)
	}

	// Create mica client first - this registers with micad but doesn't start PTY services yet
	log.LocateDebugf("Creating mica client for task %s", r.ID)
	_, err = create(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("creating mica client: %w", err)
	}

	// TODO: for debug display
	bundle := r.Bundle
	var bundleContent []os.DirEntry
	if bundle != "" {
		bundleContent, err = os.ReadDir(bundle)
		if err != nil {
			return nil, fmt.Errorf("reading bundle: %w", err)
		}
	}
	commandline := fmt.Sprintf("echo 'current CreateTaskRequest: %v; s.procs: %v; bundleContent: %v'", r, s.procs, bundleContent)
	cmd := exec.CommandContext(ctx, "sh", "-c", commandline)
	if _, err := os.Stat(filepath.Join(bundle, "config.json")); err == nil {
		commandline = fmt.Sprintf("cat %s", filepath.Join(bundle, "config.json"))
		cmd = exec.CommandContext(ctx, "sh", "-c", commandline)
	} else if _, err := os.Stat(filepath.Join(bundle, "config.v2.json")); err == nil {
		commandline = fmt.Sprintf("cat %s", filepath.Join(bundle, "config.v2.json"))
		cmd = exec.CommandContext(ctx, "sh", "-c", commandline)
	} else {
		commandline = fmt.Sprintf("echo 'no config.json or config.v2.json found in bundle: %s'", bundle)
		cmd = exec.CommandContext(ctx, "sh", "-c", commandline)
	}

	// Initialize MicaIO for future PTY communication
	// Note: PTY devices will be created later when mica client starts
	micaIO, err := libmica.NewMicaIO(ctx, r.ID, r.Stdin, r.Stdout, r.Stderr, r.Terminal)
	log.LocateDebugf("micaIO: %v", micaIO)
	if err != nil {
		return nil, fmt.Errorf("creating mica IO: %w", err)
	}

	// Start the placeholder process
	// NOTICE: only created successfully, then handle pty
	if err := cmd.Start(); err != nil {
		micaIO.Close()
		return nil, fmt.Errorf("starting placeholder process: %w", err)
	}

	defer func() {
		if retErr != nil {
			if err := micaIO.Close(); err != nil {
				log.LocateDebugf("pid = %v, err = %v", cmd.Process.Pid, err)
				log.Error("failed to close mica IO")
			}
			if cmd.Process != nil {
				// we do not care the debug commandline, just kill it.
				if err := cmd.Process.Kill(); err != nil {
					log.LocateDebugf("pid = %v, err = %v", cmd.Process.Pid, err)
					log.Error("failed to debug kill placeholder process")
				}
			}
		}
	}()

	pid := cmd.Process.Pid
	log.Debugf("Created placeholder process with PID %d for task %s", pid, r.ID)

	doneCtx, markDone := context.WithCancel(context.Background())

	go func() {
		defer markDone()

		if err := cmd.Wait(); err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				log.Errorf("failed to wait for placeholder process %d", pid)
			}
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
			log.Warn("placeholder process wait returned without setting process state")
		}

		s.m.Lock()
		defer s.m.Unlock()

		proc, ok := s.procs[r.ID]
		if !ok {
			log.Errorf("failed to write final status of done placeholder process: task was removed")
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
	pidPath := filepath.Join(filepath.Join(filepath.Dir(cwd), r.ID), initPidFile)
	if err := shim.WritePidFile(pidPath, pid); err != nil {
		return nil, fmt.Errorf("writing pid file of placeholder process: %w", err)
	}

	s.procs[r.ID] = &initProcess{
		pid:     pid,
		doneCtx: doneCtx,
		stdout:  r.Stdout,
		micaIO:  micaIO,
	}

	log.Infof("Successfully created MICA task %s with placeholder PID %d", r.ID, pid)
	return &taskAPI.CreateTaskResponse{
		Pid: uint32(pid),
	}, nil
}


// 1. search bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TODO: Only copy values, the evaluation procedure is in the caller function
// TALK: 这是预留的核，实际client可能更后面启动, 以及启动可能失败
// TODO: 现在我们全部假定是单核RTOS, mica侧还未实现多核, 但是在镜像label中，我们可以指定核数量
func CreateMicaConf(container *cntr.Container) libmica.MicaClientConf {
	info := container.GetMicaContainerInfo()

	firmware := info.FirmwarePath()
	cpu := info.CPU()
	pedestal := info.Ped()
	name := container.ID

	conf := libmica.MicaClientConf{}
	conf.Init(cpu, name, firmware, pedestal.PedestalType.String(), pedestal.PedestalConf, false)
	return conf
}

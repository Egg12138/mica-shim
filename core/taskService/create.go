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
// The init process is now a true init process :
// 1. satisfy containerd's requirements
// 2. as an agent, managing something needed in future
// TALK: the init process receives signals from containerd,
func (s *micaTaskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (_ *taskAPI.CreateTaskResponse, retErr error) {
	log.FDebugf("create id:%s", r.ID)
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
	log.FDebugf("Creating mica client for task %s", r.ID)
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
	log.FDebugf("micaIO: %v", micaIO)
	if err != nil {
		return nil, fmt.Errorf("creating mica IO: %w", err)
	}

	// Start the MICA init process
	// NOTICE: only if created successfully, handle pty
	if err := cmd.Start(); err != nil {
		micaIO.Close()
		return nil, fmt.Errorf("starting MICA init process: %w", err)
	}

	defer func() {
		if retErr != nil {
			if err := micaIO.Close(); err != nil {
				log.FDebugf("pid = %v, err = %v", cmd.Process.Pid, err)
				log.Error("failed to close mica IO")
			}
			if cmd.Process != nil {
				// Kill the MICA init process on error
				if err := cmd.Process.Kill(); err != nil {
					log.FDebugf("pid = %v, err = %v", cmd.Process.Pid, err)
					log.Error("failed to kill MICA init process")
				}
			}
		}
	}()

	pid := cmd.Process.Pid
	log.Debugf("Created MICA init process with PID %d for task %s", pid, r.ID)

	doneCtx, markDone := context.WithCancel(context.Background())

	go func() {
		defer markDone()

		if err := cmd.Wait(); err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				log.Errorf("failed to wait for MICA init process %d", pid)
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
			log.Warn("MICA init process wait returned without setting process state")
		}

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
	pidPath := filepath.Join(filepath.Join(filepath.Dir(cwd), r.ID), initPidFile)
	log.Debugf("we do created a pidFile: %s", pidPath)
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

// 1. search bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TODO: Only copy values, the evaluation procedure is in the caller function
// TALK: 这是预留的核，实际client可能更后面启动, 以及启动可能失败
// TODO: 现在我们全部假定是单核RTOS, mica侧还未实现多核, 但是在镜像label中，我们可以指定核数量
func CreateMicaConf(container *cntr.Container) (libmica.MicaClientConf, error) {
	info := container.GetMicaContainerInfo()

	firmware := info.FirmwarePath()
	pedestal := info.Ped()
	name := container.ID
	// TODO: Calculate the CPU too late, we should calculate it in the container creation
	cpu, err := container.GetClientCPU()
	if err != nil {
		return libmica.MicaClientConf{}, fmt.Errorf("failed to get client cpu: %w", err)
	}
	conf := libmica.MicaClientConf{}
	conf.Init(uint32(cpu), name, firmware, pedestal.PedestalType.String(), pedestal.PedestalConf, false)
	return conf, nil
}

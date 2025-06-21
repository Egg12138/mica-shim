package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime/v2/shim"
)

// containerd-specific environment variables set while invoking the shim's
// start command.
// https://github.com/containerd/containerd/tree/v1.7.3/runtime/v2#start:
// The start command may have the following containerd specific environment variables set:
const (
	contdShimEnvShedCore = "SCHED_CORE"
)

// Name of the file that contains the init pid.
const initPidFile = "init.pid"

// https://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html#tag_18_21_18
const exitCodeSignal = 128

// NewManager returns a new shim.Manager.
func NewManager(name string) *manager {
	return &manager{name: name}
}

// manager manages shim processes.
type manager struct {
	name string
}

var _ shim.Manager = (*manager)(nil)

// Name returns the name of the shim.
func (m *manager) Name() string {
	return m.name
}

// NOTICE: `Start` is the start of the shimv2, instead of container or task.
// Start starts a shim process.
// It implements the shim's "start" command.
// https://github.com/containerd/containerd/tree/v1.7.3/runtime/v2#start
func (*manager) Start(ctx context.Context, containerID string, opts shim.StartOpts) (addr string, retErr error) {
	// get current shim binary path to run
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("getting executable of current process: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current working directory: %w", err)
	}

	var args []string
	if opts.Debug {
		args = append(args, "-debug")
	}

	// TTRPC_ADDRESS the address of containerd's ttrpc API socket
	// GRPC_ADDRESS the address of containerd's grpc API socket (1.7+)
	// MAX_SHIM_VERSION the maximum shim version supported by the client, always 2 for shim v2 (1.7+)
	// SCHED_CORE enable core scheduling if available (1.6+)
	// NAMESPACE an optional namespace the shim is operating in or inheriting (1.7+)
	cmdCfg := &shim.CommandConfig{
		Runtime:      self,
		Address:      opts.Address,
		TTRPCAddress: opts.TTRPCAddress,
		Path:         cwd,
		SchedCore:    os.Getenv(contdShimEnvShedCore) != "",
		Args:         args,
	}

	// -namespace the namespace for the container
	// -address the address of the containerd's main grpc socket
	// -publish-binary the binary path to publish events back to containerd
	// -id the id of the container (containerID)
	// The start command, as well as all binary calls to the shim, has the bundle for the container set as the cwd.
	cmd, err := shim.Command(ctx, cmdCfg)
	if err != nil {
		return "", fmt.Errorf("creating shim command: %w", err)
	}

	// adding prefix unix://
	sockAddr, err := shim.SocketAddress(ctx, opts.Address, containerID)
	if err != nil {
		return "", fmt.Errorf("getting a socket address: %w", err)
	}

	socket, err := shim.NewSocket(sockAddr)
	if err != nil {
		switch {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		case !shim.SocketEaddrinuse(err):
			return "", fmt.Errorf("creating new shim socket: %w", err)

		case shim.CanConnect(sockAddr):
			if err := shim.WriteAddress("address", sockAddr); err != nil {
				return "", fmt.Errorf("writing socket address file: %w", err)
			}
			return sockAddr, nil
		}

		if err := shim.RemoveSocket(sockAddr); err != nil {
			return "", fmt.Errorf("removing pre-existing shim socket: %w", err)
		}

		if socket, err = shim.NewSocket(sockAddr); err != nil {
			return "", fmt.Errorf("creating new shim socket (second attempt): %w", err)
		}
	}

	defer func() {
		if retErr != nil {
			if err := socket.Close(); err != nil {
				log.G(ctx).WithError(err).Error("failed to close shim socket on start error")
			}
			if err := shim.RemoveSocket(sockAddr); err != nil {
				log.G(ctx).WithError(err).Error("removing shim socket on start error")
			}
		}
	}()

	if err := shim.WriteAddress("address", sockAddr); err != nil {
		return "", fmt.Errorf("writing socket address file: %w", err)
	}

	sockF, err := socket.File()
	if err != nil {
		return "", fmt.Errorf("getting shim socket file descriptor: %w", err)
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, sockF)

	// LEARN:
	// runtime.LockOSThread()

	// if cmdCfg.SchedCore {
	// 	if err := schedcore.Create(schedcore.ProcessGroup); err != nil {
	// 		return "", fmt.Errorf("enabling sched core support: %w", err)
	// 	}
	// }

	if err := cmd.Start(); err != nil {
		sockF.Close()
		return "", fmt.Errorf("starting shim command: %w", err)
	}

	runtime.UnlockOSThread()

	defer func() {
		if retErr != nil {
			if err := cmd.Cancel(); err != nil {
				log.G(ctx).WithError(err).Error("failed to cancel shim command")
			}
		}
	}()

	// NOTICE: cmd.Process is the process of the shim, which we should handle properly
	go func() {
		if err := cmd.Wait(); err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				log.G(ctx).WithError(err).Errorf("failed to wait for shim process %d", cmd.Process.Pid)
			}
		}
	}()

	if err := shim.AdjustOOMScore(cmd.Process.Pid); err != nil {
		return "", fmt.Errorf("adjusting shim process OOM score: %w", err)
	}

	return sockAddr, nil
}

// Stop stops a shim process.
// It implements the shim's "delete" command.
// https://github.com/containerd/containerd/tree/v1.7.3/runtime/v2#delete
func (*manager) Stop(ctx context.Context, containerID string) (shim.StopStatus, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return shim.StopStatus{}, fmt.Errorf("getting current working directory: %w", err)
	}

	pidPath := filepath.Join(filepath.Join(filepath.Dir(cwd), containerID), initPidFile)
	pid, err := readPidFile(pidPath)
	if err != nil {
		log.G(ctx).WithError(err).Warn("failed to read init pid file")
	}

	if pid > 0 {
		p, _ := os.FindProcess(pid)
		// The POSIX standard specifies that a null-signal can be sent to check
		// whether a PID is valid.
		if err := p.Signal(syscall.Signal(0)); err == nil {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				log.G(ctx).WithError(err).Warnf("failed to send kill syscall to init process %d", pid)
			}
		}
	}

	return shim.StopStatus{
		Pid:        pid,
		ExitedAt:   time.Now(),
		ExitStatus: int(exitCodeSignal + syscall.SIGKILL),
	}, nil
}

// readPidFile reads the pid file at the provided path and returns the pid it
// contains.
func readPidFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1, err
	}
	return strconv.Atoi(string(data))
}

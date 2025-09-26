package libmica

import (
	"fmt"
	er "mica-shim/errors"
	log "mica-shim/logger"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	defs "mica-shim/definitions"
)

// Constants
const MICAD_PIDFILE = defs.DaemonRoot + "/micad.pid"
const (
	DaemonRunning = "running"
	DaemonPanic   = "panic"
	DaemonStopped = "stopped"
)

// Types
type MicaDaemonState struct {
	Pid       int
	State     string
	Listening bool
}

// isMicadRunning checks if micad is already running by verifying the PID file
// and process status. Returns (pid, true) if running, (0, false) otherwise.
func isMicadRunning() (int, bool) {
	if _, err := os.Stat(MICAD_PIDFILE); err != nil {
		return 0, false
	}

	pidFile, err := os.ReadFile(MICAD_PIDFILE)
	if err != nil {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidFile)))
	if err != nil {
		return 0, false
	}

	// Check if process is running by sending signal 0
	sigProcExistence := syscall.Signal(0)
	if err := syscall.Kill(pid, sigProcExistence); err != nil {
		return pid, false
	}

	return pid, true
}

// setupMicad attempts to start micad if it's not already running
func setupMicad() error {
	if pid, running := isMicadRunning(); running {
		log.Infof("micad is already running with PID %d", pid)
		return nil
	}

	if _, err := exec.LookPath("systemctl"); err == nil {
		cmd := exec.Command("systemctl", "start", "micad")
		if err := cmd.Run(); err != nil {
			log.Warnf("failed to start micad with systemctl: %v", err)
		} else {
			log.Info("micad started via systemctl")
			return nil
		}
	}

	if _, err := exec.LookPath("service"); err == nil {
		cmd := exec.Command("service", "micad", "start")
		if err := cmd.Run(); err != nil {
			log.Warnf("failed to start micad with service: %v", err)
		} else {
			log.Info("micad started via service")
			return nil
		}
	}

	// Try to start micad directly
	micadPaths := []string{
		"/bin/micad",
		"/sbin/micad",
		"/usr/bin/micad",
		"/usr/local/bin/micad",
	}

	for _, path := range micadPaths {
		if _, err := os.Stat(path); err == nil {
			cmd := exec.Command(path)
			if err := cmd.Start(); err != nil {
				log.Warnf("failed to start micad at %s: %v", path, err)
				continue
			}
			log.Infof("micad started directly from %s", path)
			return nil
		}
	}

	return fmt.Errorf("micad not found and could not be started")
}

// TODO: when to check?
// return nil => failed to setup, no need to run micran
// return state => daemon state
func DaemonState() (*MicaDaemonState, error) {
	if defs.IsMock {
		return &MicaDaemonState{
			Pid:       114514,
			State:     DaemonRunning,
			Listening: true,
		}, nil
	}

	state := MicaDaemonState{}

	pid, running := isMicadRunning()
	if !running {
		// Try to setup micad if not running
		if setupErr := setupMicad(); setupErr != nil {
			return nil, fmt.Errorf("failed to setup micad daemon: %w", setupErr)
		}
		// Check again after setup attempt
		pid, running = isMicadRunning()
		if !running {
			state.Listening = false
			state.State = DaemonStopped
			state.Pid = 0
			return &state, er.ErrMicadNotRunning
		}
	}

	state.Pid = pid
	state.State = DaemonRunning
	state.Listening = validSocketPath(defs.MicaCreatSocketPath)

	return &state, nil
}

func (m *MicaDaemonState) Active() bool {
	if m == nil {
		return false
	}
	return m.State == DaemonRunning
}

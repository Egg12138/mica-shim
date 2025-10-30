package libmica

import (
	"fmt"
	er "mica-shim/errors"
	log "mica-shim/logger"
	"mica-shim/pkg/utils"
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

// micadDetect checks if micad is already running by verifying the PID file
// and process status. Returns (pid, instanceNum, true) if running, (0, 0, false) otherwise.
func micadDetect() (pid, instanceNum int, running bool) {
	// If MICAD_PIDFILE is missing, check the multi micad case
	if utils.FileExist(defs.MicaCreatSocketPath) {
		// Check processes using the socket path (like lsof)
		cmdName := "micad"
		if defs.IsMock {
			cmdName = "mock_mica"
		}
		pids := utils.LsofSocket(defs.MicaCreatSocketPath, cmdName)
		if len(pids) == 1 {
			return pids[0], 1, true
		} else {
			log.Debugf("lsof found multiple micad processes (%v) using socket %s", pids, defs.MicaCreatSocketPath)
			return 0, len(pids), true
		}
	}

	// believe micad MICAD_PIDFILE can avoid race
	if _, err := os.Stat(MICAD_PIDFILE); err != nil {
		return 0, 0, false
	}

	pidFile, err := os.ReadFile(MICAD_PIDFILE)
	if err != nil {
		return 0, 0, false
	}

	pidFromFile, err := strconv.Atoi(strings.TrimSpace(string(pidFile)))
	if err != nil {
		return 0, 0, false
	}

	// Check if process is running by sending signal 0
	sigProcExistence := syscall.Signal(0)
	if err := syscall.Kill(pidFromFile, sigProcExistence); err != nil {
		return pidFromFile, 1, false
	}

	return pidFromFile, 1, true
}

// setupMicad attempts to start micad if it's not already running
func setupMicad() error {
	if pid, _, running := micadDetect(); running {
		log.Debugf("micad is already running with PID %d", pid)
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

	return fmt.Errorf("mica daemon service not found or could not be started")
}

// TODO: when to check?
// return nil => failed to setup, no need to run micran
// return state => daemon state
func DaemonState() (*MicaDaemonState, error) {
	log.Info("DaemonState() called")
	state := MicaDaemonState{}

	pid, micadNum, running := micadDetect()
	if !running {
		if setupErr := setupMicad(); setupErr != nil {
			return nil, fmt.Errorf("failed to setup micad daemon: %w", setupErr)
		}
		// Check again after setup attempt
		pid, _, running = micadDetect()
		if !running {
			state.Listening = false
			state.State = DaemonStopped
			state.Pid = 0
			return &state, er.MicadNotRunning
		}
	} else if micadNum > 1 {
		return nil, fmt.Errorf("multiple micad instances detected (%d), this may cause issues", micadNum)
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

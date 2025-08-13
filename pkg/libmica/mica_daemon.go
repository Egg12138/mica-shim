package libmica

import (
	"fmt"
	"io"
	defs "mica-shim/definitions"
	"os"
	"strconv"
)

const MICAD_PIDFILE = defs.DaemonRoot + "/micad.pid"
const (
	DaemonRunning = "runnig"
	DaemonPanic   = "panic"
	DaemonStopped = "stopped"
)

type MicaDaemonState struct {
	Pid       int
	State     string
	Listening bool
}

// TODO: check more about kernel-level state
func DaemonState() (*MicaDaemonState, error) {
	pidFile, err := os.OpenFile(MICAD_PIDFILE, os.O_RDONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open mica daemon pid file: %w", err)
	}
	defer pidFile.Close()

	state := MicaDaemonState{}
	bytes, err := io.ReadAll(pidFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read mica daemon pid: %w", err)
	}
	pid, err := strconv.Atoi(string(bytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read mica daemon pid: %w", err)
	}

	state.Pid = pid

	_, err = os.FindProcess(pid)
	if err != nil {
		state.State = "stopped"
	} else {
		state.State = "running"
	}

	state.Listening = validSocketPath(defs.MicaCreatSocketPath)

	return &state, nil
}

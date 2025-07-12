package libmica

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"mica-shim/io"
	log "mica-shim/logger"
)

// PTY device mapping and discovery constants
const (
	PTYDevicePattern     = "/dev/ttyRPMSG%d"
	PTYDevicePrefix      = "/dev/ttyRPMSG"
	PTYWaitTimeout       = 30 * time.Second
	PTYDiscoveryInterval = 500 * time.Millisecond
	MaxPTYDevices        = 10
)

// MicaIO handles stdio communication between containerd and mica PTY devices
type MicaIO struct {
	taskID   string     // 任务ID
	stdin    *io.PipeIO // 标准输入管道
	stdout   *io.PipeIO // 标准输出管道
	stderr   *io.PipeIO // 标准错误管道
	terminal bool       // 是否为终端模式

	// PTY设备连接
	ptyDevice string   // PTY设备路径
	ptyFile   *os.File // PTY设备文件句柄

	// runtime side
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	started bool

	mu sync.RWMutex

	micaClientConn net.Conn
}

// PTY device discovery result
type PTYDiscoveryResult struct {
	DevicePath string
	Error      error
}

// NewMicaIO creates a new MicaIO instance for the given task
func NewMicaIO(ctx context.Context, taskID string, stdin, stdout, stderr string, terminal bool) (*MicaIO, error) {
	ctxWithCancel, cancel := context.WithCancel(ctx)

	mio := &MicaIO{
		taskID:   taskID,
		terminal: terminal,
		ctx:      ctxWithCancel,
		cancel:   cancel,
		done:     make(chan struct{}),
		started:  false,
	}

	// Initialize PipeIO for stdout if provided
	if stdout != "" {
		stdoutPipe, err := io.NewPipeIO(stdout)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("creating stdout pipe: %w", err)
		}
		mio.stdout = stdoutPipe
	}

	// Initialize PipeIO for stderr if provided
	if stderr != "" {
		stderrPipe, err := io.NewPipeIO(stderr)
		if err != nil {
			cancel()
			if mio.stdout != nil {
				mio.stdout.Close()
			}
			return nil, fmt.Errorf("creating stderr pipe: %w", err)
		}
		mio.stderr = stderrPipe
	}

	// Initialize PipeIO for stdin if provided
	if stdin != "" {
		stdinPipe, err := io.NewPipeIO(stdin)
		if err != nil {
			cancel()
			if mio.stdout != nil {
				mio.stdout.Close()
			}
			if mio.stderr != nil {
				mio.stderr.Close()
			}
			return nil, fmt.Errorf("creating stdin pipe: %w", err)
		}
		mio.stdin = stdinPipe
	}

	return mio, nil
}

// discoverPTYDevice discovers the PTY device created by micad for this task
func (mio *MicaIO) discoverPTYDevice() (*PTYDiscoveryResult, error) {
	log.Debugf("Starting PTY device discovery for task %s", mio.taskID)

	// First, try to find any existing PTY devices
	existingDevices := mio.scanExistingPTYDevices()

	// For now, use a simple strategy: use the first available device
	// TODO: Implement proper task-to-PTY mapping based on micad's assignment
	if len(existingDevices) > 0 {
		selectedDevice := existingDevices[0]
		log.Debugf("Selected PTY device %s for task %s", selectedDevice, mio.taskID)
		return &PTYDiscoveryResult{
			DevicePath: selectedDevice,
			Error:      nil,
		}, nil
	}

	return &PTYDiscoveryResult{
		DevicePath: "",
		Error:      fmt.Errorf("no PTY devices found for task %s", mio.taskID),
	}, nil
}

// scanExistingPTYDevices scans for existing PTY devices
func (mio *MicaIO) scanExistingPTYDevices() []string {
	var devices []string

	for i := 0; i < MaxPTYDevices; i++ {
		ptyPath := fmt.Sprintf(PTYDevicePattern, i)
		if stat, err := os.Stat(ptyPath); err == nil {
			// Check if it's a character device (PTY)
			if stat.Mode()&os.ModeCharDevice != 0 {
				devices = append(devices, ptyPath)
				log.Debugf("Found PTY device: %s", ptyPath)
			}
		}
	}

	return devices
}

// waitForPTYDeviceCreation waits for micad to create PTY devices
func (mio *MicaIO) waitForPTYDeviceCreation() error {
	log.Debugf("Waiting for PTY device creation for task %s", mio.taskID)

	timeout := time.After(PTYWaitTimeout)
	ticker := time.NewTicker(PTYDiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for PTY device creation for task %s", mio.taskID)

		case <-ticker.C:
			result, err := mio.discoverPTYDevice()
			if err != nil {
				log.Debugf("PTY discovery error: %v", err)
				continue
			}

			if result.DevicePath != "" {
				mio.ptyDevice = result.DevicePath
				log.Debugf("PTY device discovered: %s for task %s", result.DevicePath, mio.taskID)
				return nil
			}

		case <-mio.ctx.Done():
			return fmt.Errorf("context cancelled while waiting for PTY device")
		}
	}
}

// connectToPTY opens the PTY device for communication
func (mio *MicaIO) connectToPTY() error {
	if mio.ptyDevice == "" {
		if err := mio.waitForPTYDeviceCreation(); err != nil {
			return fmt.Errorf("waiting for PTY device creation: %w", err)
		}
	}

	// Open the PTY device with read-write access
	ptyFile, err := os.OpenFile(mio.ptyDevice, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return fmt.Errorf("opening PTY device %s: %w", mio.ptyDevice, err)
	}

	mio.ptyFile = ptyFile
	log.Infof("Successfully connected to PTY device %s for task %s", mio.ptyDevice, mio.taskID)
	return nil
}

// Start begins the IO forwarding between containerd and PTY device
func (mio *MicaIO) Start() error {
	mio.mu.Lock()
	defer mio.mu.Unlock()

	if mio.started {
		return fmt.Errorf("MicaIO already started for task %s", mio.taskID)
	}

	log.Debugf("Starting MicaIO for task %s", mio.taskID)

	if err := mio.connectToPTY(); err != nil {
		return fmt.Errorf("connecting to PTY: %w", err)
	}

	var wg sync.WaitGroup

	// Start stdout forwarding (PTY -> containerd)
	if mio.stdout != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mio.forwardPTYToStdout(); err != nil {
				log.Errorf("stdout forwarding error for task %s: %v", mio.taskID, err)
			}
		}()
	}

	// Start stderr forwarding (if separate from stdout)
	if mio.stderr != nil && !mio.terminal {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mio.forwardPTYToStderr(); err != nil {
				log.Errorf("stderr forwarding error for task %s: %v", mio.taskID, err)
			}
		}()
	}

	// Start stdin forwarding (containerd -> PTY)
	if mio.stdin != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mio.forwardStdinToPTY(); err != nil {
				log.Errorf("stdin forwarding error for task %s: %v", mio.taskID, err)
			}
		}()
	}

	// Start the containerd pipe copying
	if mio.stdout != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mio.stdout.Copy(mio.ctx); err != nil {
				log.Errorf("stdout pipe copy error for task %s: %v", mio.taskID, err)
			}
		}()
	}

	if mio.stderr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mio.stderr.Copy(mio.ctx); err != nil {
				log.Errorf("stderr pipe copy error for task %s: %v", mio.taskID, err)
			}
		}()
	}

	if mio.stdin != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mio.stdin.Copy(mio.ctx); err != nil {
				log.Errorf("stdin pipe copy error for task %s: %v", mio.taskID, err)
			}
		}()
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(mio.done)
	}()

	mio.started = true
	log.Infof("MicaIO started successfully for task %s using PTY device %s", mio.taskID, mio.ptyDevice)
	return nil
}

// forwardPTYToStdout forwards data from PTY to containerd stdout
func (mio *MicaIO) forwardPTYToStdout() error {
	if mio.ptyFile == nil || mio.stdout == nil {
		return nil
	}

	log.Debugf("Starting PTY->stdout forwarding for task %s", mio.taskID)

	buf := make([]byte, 4096)
	writer := mio.stdout.Writer()

	for {
		select {
		case <-mio.ctx.Done():
			log.Debugf("PTY->stdout forwarding stopped for task %s: context done", mio.taskID)
			return mio.ctx.Err()
		default:
			// Set read timeout to allow context checking
			mio.ptyFile.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

			n, err := mio.ptyFile.Read(buf)
			if err != nil {
				if os.IsTimeout(err) {
					continue // Timeout is expected, continue to check context
				}
				log.Debugf("PTY read error for task %s: %v", mio.taskID, err)
				return fmt.Errorf("reading from PTY: %w", err)
			}

			if n > 0 {
				log.Debugf("Forwarding %d bytes from PTY to stdout for task %s", n, mio.taskID)
				if _, err := writer.Write(buf[:n]); err != nil {
					return fmt.Errorf("writing to stdout: %w", err)
				}
			}
		}
	}
}

// forwardPTYToStderr forwards data from PTY to containerd stderr
func (mio *MicaIO) forwardPTYToStderr() error {
	if mio.ptyFile == nil || mio.stderr == nil {
		return nil
	}

	// For terminal mode, stderr is typically combined with stdout
	// For non-terminal mode, we could implement separate stderr handling
	log.Debugf("Stderr forwarding not implemented for task %s (terminal mode: %v)", mio.taskID, mio.terminal)
	return nil
}

// forwardStdinToPTY forwards data from containerd stdin to PTY
func (mio *MicaIO) forwardStdinToPTY() error {
	if mio.ptyFile == nil || mio.stdin == nil {
		return nil
	}

	log.Debugf("Starting stdin->PTY forwarding for task %s", mio.taskID)

	// For now, we implement a basic stdin forwarding mechanism
	// This requires more sophisticated handling depending on how stdin is provided

	// TODO: Implement proper stdin reading from containerd and writing to PTY
	// This might require changes to the PipeIO interface or additional mechanisms

	log.Debugf("Stdin->PTY forwarding placeholder for task %s", mio.taskID)
	return nil
}

// Wait waits for all IO operations to complete
func (mio *MicaIO) Wait() {
	<-mio.done
}

// Close closes all IO resources
func (mio *MicaIO) Close() error {
	log.Debugf("Closing MicaIO for task %s", mio.taskID)

	mio.cancel()

	var errs []error

	mio.mu.Lock()
	defer mio.mu.Unlock()

	if mio.ptyFile != nil {
		if err := mio.ptyFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing PTY file: %w", err))
		}
		mio.ptyFile = nil
	}

	if mio.stdout != nil {
		if err := mio.stdout.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing stdout: %w", err))
		}
	}

	if mio.stderr != nil {
		if err := mio.stderr.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing stderr: %w", err))
		}
	}

	if mio.stdin != nil {
		if err := mio.stdin.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing stdin: %w", err))
		}
	}

	mio.started = false

	if len(errs) > 0 {
		return fmt.Errorf("errors closing MicaIO for task %s: %v", mio.taskID, errs)
	}

	log.Debugf("MicaIO closed successfully for task %s", mio.taskID)
	return nil
}

// GetPTYDevice returns the PTY device path (for debugging/info)
func (mio *MicaIO) GetPTYDevice() string {
	mio.mu.RLock()
	defer mio.mu.RUnlock()
	return mio.ptyDevice
}

// IsStarted returns whether the MicaIO has been started
func (mio *MicaIO) IsStarted() bool {
	mio.mu.RLock()
	defer mio.mu.RUnlock()
	return mio.started
}

// GetTaskID returns the task ID
func (mio *MicaIO) GetTaskID() string {
	return mio.taskID
}

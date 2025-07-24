package libmica

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	ioutils "mica-shim/io"
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
	taskID   string               // Task identifier
	stdin    *ioutils.PipeIO     // Stdin pipe
	stdout   *ioutils.PipeIO     // Stdout pipe
	stderr   *ioutils.PipeIO     // Stderr pipe
	terminal bool                // Terminal mode flag

	// PTY device connection
	ptyDevice string   // PTY device path
	ptyFile   *os.File // PTY device file handle

	// Runtime state
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	started bool

	mu sync.RWMutex

	micaClientConn net.Conn
	
	// Direct FIFO path for stdin forwarding
	stdinFIFOPath string
}

// PTYDiscoveryResult contains PTY device discovery result
type PTYDiscoveryResult struct {
	DevicePath string
	Error      error
}

// stdinFIFOReader handles stdin FIFO reading
type stdinFIFOReader struct {
	file   *os.File
	taskID string
}

// newStdinFIFOReader creates a stdin FIFO reader
func newStdinFIFOReader(stdinPath, taskID string) (*stdinFIFOReader, error) {
	if stdinPath == "" {
		return nil, fmt.Errorf("stdin path is empty")
	}

	file, err := os.OpenFile(stdinPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("opening stdin FIFO %s: %w", stdinPath, err)
	}

	return &stdinFIFOReader{
		file:   file,
		taskID: taskID,
	}, nil
}

// Read reads data from stdin FIFO
func (r *stdinFIFOReader) Read(buf []byte) (int, error) {
	return r.file.Read(buf)
}

// SetReadDeadline sets read deadline for stdin FIFO
func (r *stdinFIFOReader) SetReadDeadline(t time.Time) error {
	return r.file.SetReadDeadline(t)
}

// Close closes the stdin FIFO
func (r *stdinFIFOReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// NewMicaIO creates a new MicaIO instance
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

	// Store stdin FIFO path for direct access
	if stdin != "" {
		mio.stdinFIFOPath = stdin
		stdinPipe, err := ioutils.NewPipeIO(stdin)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("creating stdin pipe: %w", err)
		}
		mio.stdin = stdinPipe
	}

	// Initialize stdout pipe
	if stdout != "" {
		stdoutPipe, err := ioutils.NewPipeIO(stdout)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("creating stdout pipe: %w", err)
		}
		mio.stdout = stdoutPipe
	}

	// Initialize stderr pipe
	if stderr != "" {
		stderrPipe, err := ioutils.NewPipeIO(stderr)
		if err != nil {
			cancel()
			if mio.stdout != nil {
				mio.stdout.Close()
			}
			return nil, fmt.Errorf("creating stderr pipe: %w", err)
		}
		mio.stderr = stderrPipe
	}

	return mio, nil
}

// discoverPTYDevice discovers PTY device created by micad
func (mio *MicaIO) discoverPTYDevice() (*PTYDiscoveryResult, error) {
	log.Debugf("Starting PTY device discovery for task %s", mio.taskID)

	existingDevices := mio.scanExistingPTYDevices()

	// Use first available device (TODO: implement proper task-to-PTY mapping)
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

// connectToPTY opens PTY device for communication
func (mio *MicaIO) connectToPTY() error {
	if mio.ptyDevice == "" {
		if err := mio.waitForPTYDeviceCreation(); err != nil {
			return fmt.Errorf("waiting for PTY device creation: %w", err)
		}
	}

	ptyFile, err := os.OpenFile(mio.ptyDevice, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return fmt.Errorf("opening PTY device %s: %w", mio.ptyDevice, err)
	}

	mio.ptyFile = ptyFile
	log.Infof("Successfully connected to PTY device %s for task %s", mio.ptyDevice, mio.taskID)
	return nil
}

// Start begins IO forwarding between containerd and PTY device
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

	// Start stderr forwarding (PTY -> containerd)
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
	if mio.stdinFIFOPath != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mio.forwardStdinToPTY(); err != nil {
				log.Errorf("stdin forwarding error for task %s: %v", mio.taskID, err)
			}
		}()
	}

	// Start containerd pipe copying
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

	// Note: stdin.Copy() not used - we handle stdin via direct FIFO reading

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
			mio.ptyFile.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

			n, err := mio.ptyFile.Read(buf)
			if err != nil {
				if os.IsTimeout(err) {
					continue
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

	// Stderr is typically combined with stdout in terminal mode
	log.Debugf("Stderr forwarding not implemented for task %s (terminal mode: %v)", mio.taskID, mio.terminal)
	return nil
}

// forwardStdinToPTY forwards data from containerd stdin to PTY
func (mio *MicaIO) forwardStdinToPTY() error {
	if mio.ptyFile == nil {
		return nil
	}

	log.Debugf("Starting stdin->PTY forwarding for task %s", mio.taskID)

	stdinReader, err := newStdinFIFOReader(mio.stdinFIFOPath, mio.taskID)
	if err != nil {
		return fmt.Errorf("creating stdin FIFO reader: %w", err)
	}
	defer stdinReader.Close()

	buf := make([]byte, 4096)
	for {
		select {
		case <-mio.ctx.Done():
			log.Debugf("stdin->PTY forwarding stopped for task %s: context done", mio.taskID)
			return mio.ctx.Err()
		default:
			stdinReader.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

			n, err := stdinReader.Read(buf)
			if err != nil {
				if os.IsTimeout(err) {
					continue
				}
				if err == io.EOF {
					log.Debugf("stdin EOF reached for task %s", mio.taskID)
					return nil
				}
				log.Debugf("stdin read error for task %s: %v", mio.taskID, err)
				return fmt.Errorf("reading from stdin: %w", err)
			}

			if n > 0 {
				log.Debugf("Forwarding %d bytes from stdin to PTY for task %s", n, mio.taskID)
				
				// Write to PTY with retry mechanism
				written := 0
				for written < n {
					mio.ptyFile.SetWriteDeadline(time.Now().Add(1 * time.Second))
					
					bytesWritten, err := mio.ptyFile.Write(buf[written:n])
					if err != nil {
						if os.IsTimeout(err) {
							log.Debugf("PTY write timeout for task %s, retrying", mio.taskID)
							continue
						}
						return fmt.Errorf("writing to PTY: %w", err)
					}
					written += bytesWritten
				}
				
				log.Debugf("Successfully wrote %d bytes to PTY for task %s", written, mio.taskID)
			}
		}
	}
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

// GetPTYDevice returns the PTY device path
func (mio *MicaIO) GetPTYDevice() string {
	mio.mu.RLock()
	defer mio.mu.RUnlock()
	return mio.ptyDevice
}

// IsStarted returns whether MicaIO has been started
func (mio *MicaIO) IsStarted() bool {
	mio.mu.RLock()
	defer mio.mu.RUnlock()
	return mio.started
}

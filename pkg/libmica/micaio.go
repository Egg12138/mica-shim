package libmica

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	log "micrun/logger"
	ioutils "micrun/pkg/io"
)

// Constants
// PTY device mapping and discovery constants
const (
	PTYDevLegacyPattern  = "/dev/ttyRPMSG%d"
	PTYDevPattern        = "/dev/ttyRPMSG_%s_0"
	PTYWaitTimeout       = 30 * time.Second
	PTYDiscoveryInterval = 500 * time.Millisecond
	MaxPTYDevLegacyNum   = 10
)

// Types
// MicaIO handles stdio communication between containerd and mica PTY devices
type MicaIO struct {
	taskID   string          // Task identifier
	stdin    *ioutils.PipeIO // Stdin pipe
	stdout   *ioutils.PipeIO // Stdout pipe
	stderr   *ioutils.PipeIO // Stderr pipe
	terminal bool            // Terminal mode flag

	// PTY device connection
	ptyDevice string   // PTY device path
	ptyFile   *os.File // PTY device file handle

	// Optional override for PTY device selection (via env MICRAN_PTY_DEVICE)
	ptyDeviceOverride string

	// Runtime state
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	started bool

	mu sync.RWMutex

	// stdin FIFO reader that is actively forwarding data to the PTY.
	// Stored so we can close it during shutdown to unblock the read loop.
	stdinReader *stdinFIFOReader

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

// Constructors
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

	// Read PTY device override from environment if provided
	if v := os.Getenv("MICRAN_PTY_DEVICE"); v != "" {
		mio.ptyDeviceOverride = v
		log.Debugf("MicaIO: using MICRAN_PTY_DEVICE override: %s", v)
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

// stdinFIFOReader methods
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

func (mio *MicaIO) setActiveStdinReader(reader *stdinFIFOReader) {
	mio.mu.Lock()
	mio.stdinReader = reader
	mio.mu.Unlock()
}

func (mio *MicaIO) clearActiveStdinReader(reader *stdinFIFOReader) {
	mio.mu.Lock()
	if mio.stdinReader == reader {
		mio.stdinReader = nil
	}
	mio.mu.Unlock()
}

func (mio *MicaIO) closeActiveStdinReader() {
	var reader *stdinFIFOReader
	mio.mu.Lock()
	reader = mio.stdinReader
	mio.stdinReader = nil
	mio.mu.Unlock()

	if reader != nil {
		if err := reader.Close(); err != nil {
			log.Debugf("closing active stdin reader for task %s: %v", mio.taskID, err)
		}
	}
}

// MicaIO methods
// discoverPTYDevice discovers PTY device created by micad
func (mio *MicaIO) discoverPTYDevice() (*PTYDiscoveryResult, error) {
	log.Debugf("starting PTY device discovery for task %s", mio.taskID)

	existingDevices := mio.scanExistingPTYDevices()

	// Use first available device (TODO: implement proper task-to-PTY mapping)
	if len(existingDevices) > 0 {
		selectedDevice := existingDevices[0]
		log.Debugf("selected PTY device %s for task %s", selectedDevice, mio.taskID)
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

	for i := 0; i < MaxPTYDevLegacyNum; i++ {
		ptyPath := fmt.Sprintf(PTYDevLegacyPattern, i)
		if stat, err := os.Stat(ptyPath); err == nil {
			if stat.Mode()&os.ModeCharDevice != 0 {
				devices = append(devices, ptyPath)
				log.Debugf("found PTY device: %s", ptyPath)
			}
		}
	}

	return devices
}

// waitForPTYDeviceCreation waits for micad to create PTY devices
func (mio *MicaIO) waitForPTYDeviceCreation() error {
	log.Debugf("waiting for PTY device creation for task %s", mio.taskID)

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
		if mio.ptyDeviceOverride != "" {
			mio.ptyDevice = mio.ptyDeviceOverride
			log.Debugf("MicaIO: using PTY override device %s for task %s", mio.ptyDevice, mio.taskID)
		} else {
			if err := mio.waitForPTYDeviceCreation(); err != nil {
				return fmt.Errorf("waiting for PTY device creation: %w", err)
			}
		}
	}

	ptyFile, err := os.OpenFile(mio.ptyDevice, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return fmt.Errorf("opening PTY device %s: %w", mio.ptyDevice, err)
	}

	mio.ptyFile = ptyFile
	log.Debugf("successfully connected to PTY device %s for task %s", mio.ptyDevice, mio.taskID)
	return nil
}

// Start begins IO forwarding between containerd and PTY device
func (mio *MicaIO) Start() error {
	mio.mu.Lock()
	defer mio.mu.Unlock()

	if mio.started {
		return fmt.Errorf("MicaIO already started for task %s", mio.taskID)
	}

	log.Debugf("starting MicaIO for task %s", mio.taskID)

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
			if err := mio.stdout.Copy(mio.ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Errorf("stdout pipe copy error for task %s: %v", mio.taskID, err)
			}
		}()
	}

	if mio.stderr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mio.stderr.Copy(mio.ctx); err != nil && !errors.Is(err, context.Canceled) {
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
	log.Debugf("MicaIO started successfully for task %s using PTY device %s", mio.taskID, mio.ptyDevice)
	return nil
}

// forwardPTYToStdout forwards data from PTY to containerd stdout
func (mio *MicaIO) forwardPTYToStdout() error {
	if mio.ptyFile == nil || mio.stdout == nil {
		return nil
	}

	log.Debugf("starting PTY->stdout forwarding for task %s", mio.taskID)

	buf := make([]byte, 32*1024)
	writer := mio.stdout.Writer()

	for {
		n, err := mio.ptyFile.Read(buf)
		if n > 0 {
			if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
				if isWriterClosedError(writeErr) {
					log.Debugf("stdout writer closed for task %s, stopping PTY forward", mio.taskID)
					return nil
				}
				return fmt.Errorf("writing to stdout: %w", writeErr)
			}
		}

		if err != nil {
			if shouldRetryOnInterrupt(err) {
				continue
			}
			if isTemporaryUnavailable(err) {
				if mio.waitWithContext(15 * time.Millisecond) {
					return nil
				}
				continue
			}
			if isStreamClosed(err) {
				log.Debugf("PTY stream closed for task %s", mio.taskID)
				return nil
			}
			return fmt.Errorf("reading from PTY: %w", err)
		}

		if n == 0 {
			if mio.waitWithContext(10 * time.Millisecond) {
				return nil
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
	log.Debugf("stderr forwarding not implemented for task %s (terminal mode: %v)", mio.taskID, mio.terminal)
	return nil
}

// forwardStdinToPTY forwards data from containerd stdin to PTY
func (mio *MicaIO) forwardStdinToPTY() error {
	if mio.ptyFile == nil {
		return nil
	}
	if mio.stdinFIFOPath == "" {
		return nil
	}

	log.Debugf("starting stdin->PTY forwarding for task %s", mio.taskID)

	stdinReader, err := newStdinFIFOReader(mio.stdinFIFOPath, mio.taskID)
	if err != nil {
		return fmt.Errorf("creating stdin FIFO reader: %w", err)
	}
	mio.setActiveStdinReader(stdinReader)
	defer func() {
		mio.clearActiveStdinReader(stdinReader)
		stdinReader.Close()
	}()

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-mio.ctx.Done():
			log.Debugf("stdin->PTY forwarding stopped for task %s: context done", mio.taskID)
			return nil
		default:
		}

		n, readErr := stdinReader.Read(buf)
		if n > 0 {
			if writeErr := mio.writeBytesToPTY(buf[:n]); writeErr != nil {
				if isStreamClosed(writeErr) || errors.Is(writeErr, context.Canceled) {
					log.Debugf("stdin writer closed for task %s", mio.taskID)
					return nil
				}
				if isTemporaryUnavailable(writeErr) || shouldRetryOnInterrupt(writeErr) {
					if mio.waitWithContext(15 * time.Millisecond) {
						return nil
					}
					continue
				}
				return fmt.Errorf("writing to PTY: %w", writeErr)
			}
			log.Debugf("successfully wrote %d bytes to PTY for task %s", n, mio.taskID)
		}

		if readErr != nil {
			if readErr == io.EOF || isStreamClosed(readErr) {
				log.Debugf("stdin EOF reached for task %s", mio.taskID)
				return nil
			}
			if shouldRetryOnInterrupt(readErr) {
				continue
			}
			if isTemporaryUnavailable(readErr) {
				if mio.waitWithContext(15 * time.Millisecond) {
					return nil
				}
				continue
			}
			return fmt.Errorf("reading from stdin: %w", readErr)
		}
	}
}

// Wait waits for all IO operations to complete
func (mio *MicaIO) Wait() {
	<-mio.done
}

// Close closes all IO resources
func (mio *MicaIO) Close() error {
	log.Debugf("closing MicaIO for task %s", mio.taskID)

	mio.cancel()
	mio.closeActiveStdinReader()

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

func (mio *MicaIO) writeBytesToPTY(data []byte) error {
	total := 0
	for total < len(data) {
		n, err := mio.ptyFile.Write(data[total:])
		if n > 0 {
			total += n
		}
		if err != nil {
			if shouldRetryOnInterrupt(err) {
				continue
			}
			if isTemporaryUnavailable(err) {
				if mio.waitWithContext(10 * time.Millisecond) {
					return mio.ctx.Err()
				}
				continue
			}
			if isStreamClosed(err) {
				return err
			}
			return err
		}
	}
	return nil
}

func (mio *MicaIO) waitWithContext(delay time.Duration) bool {
	if delay <= 0 {
		delay = 10 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-mio.ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}

func shouldRetryOnInterrupt(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if errno == syscall.EINTR {
			return true
		}
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return shouldRetryOnInterrupt(pathErr.Err)
	}
	return false
}

func isTemporaryUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK {
			return true
		}
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return isTemporaryUnavailable(pathErr.Err)
	}
	return false
}

func isStreamClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EPIPE, syscall.EIO, syscall.EBADF, syscall.ENODEV:
			return true
		}
	}

	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return isStreamClosed(pathErr.Err)
	}
	return false
}

func isWriterClosedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
		return true
	}
	return isStreamClosed(err)
}

// Package shim provides the implementation of the containerd shim v2 interface for micran.
package shim

import (
	"context"
	"errors"
	"fmt"
	"io"
	defs "micrun/definitions"
	log "micrun/logger"
	cntr "micrun/pkg/micantainer"
	"net/url"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/fifo"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/execabs"
)

// container holds the shim's representation of a container instance.
type container struct {
	s           *shimService
	ttyio       *ttyIO
	spec        *specs.Spec
	exitTime    time.Time
	exitIOch    chan struct{}
	exitOnce    sync.Once
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	id          string
	stdin       string
	stdout      string // All output from the RTOS console is directed here.
	stderr      string
	bundle      string
	cType       cntr.ContainerType
	exit        uint32 // The exit status code.
	status      task.Status
	terminal    bool
	mounted     bool
	pid         uint32
	execs       map[string]*execProcess
}

type execProcess struct {
	id  string
	pid uint32
	// we use contaienrd shim task status to represent process status
	status     task.Status
	exitStatus uint32
	exitTime   time.Time
	waitCh     chan struct{}
	waitOnce   sync.Once

	// stdio from ExecProcessRequest; used to bridge to container PTY
	stdin    string
	stdout   string
	stderr   string
	terminal bool

	// IO bridging for exec session
	ttyio       *ttyIO
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	exitIOch    chan struct{}
}

func newExecProcess(id string) *execProcess {
	return &execProcess{
		id:          id,
		status:      task.Status_CREATED,
		waitCh:      make(chan struct{}),
		stdinCloser: make(chan struct{}),
		exitIOch:    make(chan struct{}),
	}
}

func (p *execProcess) markStarted(pid uint32) {
	p.pid = pid
	p.status = task.Status_RUNNING
}

func (p *execProcess) markExited(exitStatus uint32) (changed bool) {
	if p.status != task.Status_STOPPED {
		p.status = task.Status_STOPPED
		p.exitStatus = exitStatus
		p.exitTime = time.Now()
		changed = true
	}
	p.waitOnce.Do(func() {
		close(p.waitCh)
	})
	return changed
}

// newContainer creates a new container object for the shim.
func newContainer(s *shimService, r *taskAPI.CreateTaskRequest, cType cntr.ContainerType, ocispec *specs.Spec, mounted bool) (*container, error) {
	if r == nil {
		return nil, errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "CreateTaskRequest points to nil")
	}

	if ocispec == nil {
		ocispec = &specs.Spec{}
	}

	c := &container{
		s:           s,
		spec:        ocispec,
		exitIOch:    make(chan struct{}),
		stdinCloser: make(chan struct{}),
		id:          r.ID,
		stdin:       r.Stdin,
		stdout:      r.Stdout,
		stderr:      r.Stderr,
		bundle:      r.Bundle,
		cType:       cType,
		status:      task.Status_CREATED,
		terminal:    r.Terminal,
		mounted:     mounted,
		pid:         shimPid,
		execs:       make(map[string]*execProcess),
	}

	return c, nil
}

func (c *container) ioExit() {
	log.Debugf("received exit signal")
	if c == nil {
		return
	}
	c.exitOnce.Do(func() {
		close(c.exitIOch)
	})
}

// stdio defines the standard IO paths for a container.
type stdio struct {
	Stdin    string
	Stdout   string
	Stderr   string
	Terminal bool
}

// IO defines the interface for handling container IO streams.
type IO interface {
	io.Closer
	Stdin() io.ReadCloser
	// NOTE: stdout() and stderr() are the same writer for our current IO components.
	Stdout() io.Writer
	Stderr() io.Writer
}

// ttyIO manages the TTY and IO streams for a container.
type ttyIO struct {
	io     IO
	stream *stdio
}

func (tty *ttyIO) close() {
	tty.io.Close()
}

// newTtyIO creates a new TTY IO handler based on the provided URI scheme.
func newTtyIO(ctx context.Context, id, stdin, stdout, stderr string, terminal bool) (*ttyIO, error) {
	// TODO: Implement this function.
	var err error
	var ioImpl IO
	stream := &stdio{
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stderr,
		Terminal: terminal,
	}

	// Containerd's default IO URI is fifo.
	uri, err := url.Parse(stdout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse stdout uri: %w", err)
	}

	if uri.Scheme == "" {
		uri.Scheme = "fifo"
	}

	log.Debugf("URI parsed => %+v", uri)
	switch uri.Scheme {
	case "fifo":
		ioImpl, err = newPipeIO(ctx, stream)
	case "binary":
		log.Debugf("using binary IO for container %s", id)
		ioImpl, err = newBinaryIO(ctx, id, uri)
	case "file":
		log.Debugf("using file IO for container %s", id)
		ioImpl, err = newFileIO(ctx, stream, uri)
	default:
		return nil, fmt.Errorf("unknown STDIO scheme %s", uri.Scheme)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create io stream: %w", err)
	}

	return &ttyIO{
		io:     ioImpl,
		stream: stream,
	}, nil
}

var (
	_ IO = &pipeIO{}
	_ IO = &binaryIO{}
	_ IO = &fileIO{}
)

// pipeIO implements IO for FIFO pipes.
type pipeIO struct {
	in  io.ReadCloser
	out io.WriteCloser
}

// binaryIO implements IO by running a custom binary for logging.
// NOTE: Related code is from https://github.com/containerd/containerd/blob/v1.6.6/pkg/process/io.go#L311
type binaryIO struct {
	cmd *execabs.Cmd
	out *pipe
}

// fileIO implements IO for files, supporting writing stdout/stderr to the same file.
type fileIO struct {
	out io.WriteCloser
	in  string // File path.
}

// newPipeIO creates a new pipe-based IO handler.
func newPipeIO(ctx context.Context, stdio *stdio) (*pipeIO, error) {
	var in io.ReadCloser
	var out io.WriteCloser
	var err error
	if stdio.Stdin != "" {
		fifoFlags := syscall.O_RDONLY | syscall.O_NONBLOCK
		perm := os.FileMode(0) // Default perm, let containerd set it.
		in, err = fifo.OpenFifo(ctx, stdio.Stdin, fifoFlags, perm)
		if err != nil {
			return nil, err
		}
	}

	if stdio.Stdout != "" {
		out, err = fifo.OpenFifo(ctx, stdio.Stdout, syscall.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
	}

	pipeIO := &pipeIO{
		in:  in,
		out: out,
	}

	return pipeIO, nil
}

func newFileIO(ctx context.Context, stdio *stdio, uri *url.URL) (*fileIO, error) {
	return nil, errdefs.ErrNotImplemented
}

func newBinaryIO(ctx context.Context, id string, uri *url.URL) (bio *binaryIO, err error) {
	return nil, errdefs.ErrNotImplemented
}

func (p *pipeIO) Close() error {
	var err error
	if err = p.in.Close(); err != nil {
		return fmt.Errorf("failed to close stdin: %w", err)
	}
	if err = p.out.Close(); err != nil && p.out != nil {
		return fmt.Errorf("failed to close stdout: %w", err)
	}
	return nil
}

func (p *pipeIO) Stdin() io.ReadCloser {
	return p.in
}

func (p *pipeIO) Stdout() io.Writer {
	return p.out
}

func (p *pipeIO) Stderr() io.Writer {
	return p.out
}

func (b *binaryIO) Close() error {
	err0 := b.out.Close()
	err1 := b.cmd.Cancel()
	return errors.Join(err0, err1)
}

func (b *binaryIO) Stdin() io.ReadCloser {
	return nil
}

func (b *binaryIO) Stdout() io.Writer {
	return b.out.w
}

func (b *binaryIO) Stderr() io.Writer {
	return b.out.w
}

func (f *fileIO) Close() error {
	var err error
	if err = f.out.Close(); err != nil && f.out != nil {
		return err
	}
	return nil
}

func (f *fileIO) Stdin() io.ReadCloser {
	return nil
}

func (f *fileIO) Stdout() io.Writer {
	return f.out
}

func (f *fileIO) Stderr() io.Writer {
	return f.out
}

// newPipe creates a new OS pipe.
func newPipe() (*pipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &pipe{
		r: r,
		w: w,
	}, nil
}

// pipe is a wrapper around an OS pipe.
type pipe struct {
	r *os.File
	w *os.File
}

func (p *pipe) Close() error {
	errw := p.w.Close()
	errr := p.r.Close()
	return errors.Join(errw, errr)
}

// ioCopy manages copying data between the container's IO streams and the PTY.
func ioCopy(exitch, stdinCloser chan struct{}, tty *ttyIO, stdinPipe io.WriteCloser, stdoutPipe io.Reader) {
	var wg sync.WaitGroup

	// Since the RTOS doesn't distinguish stderr, we copy from the PTY stdout
	// to both stdout and stderr to ensure containerd receives the same output on both streams.
	if tty.io.Stdout() != nil {
		wg.Add(1)
		go func() {
			log.Debug("Starting stdout copy from PTY to containerd.")
			io.Copy(tty.io.Stdout(), stdoutPipe)
			log.Debug("Stdout copy completed.")
			wg.Done()
			if tty.io.Stdin() != nil {
				tty.io.Stdin().Close()
			}
			log.Info("Out stream copy exited.")
		}()
	}

	if tty.io.Stdin() != nil {
		wg.Add(1)
		go func() {
			log.Debug("Starting stdin copy from containerd to PTY.")
			// TALK: Maybe CopyBuffer with a buffer pool is a better choice?
			io.Copy(stdinPipe, tty.io.Stdin())
			log.Debug("Stdin copy completed.")
			close(stdinCloser)
			wg.Done()
			log.Info("Stdin io stream copy exited.")
		}()
	}

	wg.Wait()
	close(exitch)
	log.Debug("All IO copies completed.")
}

// getBoolAnnotation parses a boolean annotation from the container spec with a default value.
// Returns (value, isExplicitlySet) where isExplicitlySet indicates if the annotation was provided.
func getBoolAnnotation(spec *specs.Spec, key string, defaultValue bool) (bool, bool) {
	if spec == nil || spec.Annotations == nil {
		return defaultValue, false
	}

	if value, ok := spec.Annotations[key]; ok {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed, true
		}
		log.Warnf("Failed to parse boolean annotation, using default: %v", defaultValue)
	}
	return defaultValue, false
}

// getDurationAnnotation parses a duration annotation (in seconds) from the container spec with a default value.
// Returns (value, isExplicitlySet) where isExplicitlySet indicates if the annotation was provided.
func getDurationAnnotation(spec *specs.Spec, key string, defaultValue time.Duration) (time.Duration, bool) {
	if spec == nil || spec.Annotations == nil {
		return defaultValue, false
	}

	if value, ok := spec.Annotations[key]; ok {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			duration := time.Duration(seconds) * time.Second
			if duration > 0 {
				return duration, true
			}
			log.Warnf("annotation %s has invalid duration %s, using default %v", key, value, defaultValue)
		} else {
			log.Warnf("annotation %s parse error: %v, defaulting to %v", key, err, defaultValue)
		}
	}
	return defaultValue, false
}

// waitContainerExit waits for the container to exit and updates its status.
func waitContainerExit(ctx context.Context, s *shimService, c *container) (int32, error) {
	// Wait for IO streams to close, or mock an exit after a timeout since micad
	// cannot yet detect client OS exit.
	defaultTimeout := 30 * time.Second
	ptyAutoClose, ptyAutoCloseSet := getBoolAnnotation(c.spec, defs.PtyAutoClose, true) // Default to true for backward compatibility
	ptyTimeout, timeoutSet := getDurationAnnotation(c.spec, defs.PtyAutoCloseTimeout, defaultTimeout)

	// If pty_auto_disconnect is explicitly set to false, disable auto disconnect even if timeout is provided
	// If timeout is explicitly set but pty_auto_disconnect is not set, enable auto disconnect
	if ptyAutoCloseSet {
		// pty_auto_disconnect is explicitly set, use its value
	} else if timeoutSet {
		// timeout is set but pty_auto_disconnect is not explicitly set, enable auto disconnect
		ptyAutoClose = true
	}

	// TODO: finish mica RTOS notifier
	ptyAutoClose = true

	if c.cType.IsCriSandbox() || !ptyAutoClose {
		// Pod infra(e.g. pause) containers must remain alive until the runtime explicitly
		// tears them down (e.g. via Kill/Delete). Block here until we receive
		// that signal.
		<-c.exitIOch // block until MicRun knows client exited
		log.Debugf("received explicit exit signal for infra container %s.", c.id)
	} else if ptyAutoClose {
		select {
		case <-c.exitIOch:
			log.Debugf("The container %s IO streams closed.", c.id)
		case <-time.After(ptyTimeout):
			log.Debugf("Auto-disconnect %s terminal after %v timeout.", c.id, ptyTimeout)
		}
	}

	timeStamp := time.Now()
	ret := okExitCode

	s.mu.Lock()
	// Update container status and exit information.
	if c.cType.CanBeSandbox() {

		if s.sandbox != nil {
			sandboxID := s.sandbox.SandboxID()
			if err := s.sandbox.Stop(ctx, true); err != nil {
				log.Errorf("Failed to stop sandbox %s.", sandboxID)
			}

			if err := s.sandbox.Delete(ctx); err != nil {
				log.Errorf("Failed to delete sandbox %s.", sandboxID)
			}
		} else {
			log.Debugf("Sandbox already deleted, skipping stop/delete in waitContainerExit")
		}
	} else {
		if s.sandbox != nil {
			if _, err := s.sandbox.StopContainer(ctx, c.id, true); err != nil {
				log.Errorf("Failed to stop pod container %s.", c.id)
			}
		} else {
			log.Debugf("Sandbox already deleted, skipping StopContainer for %s", c.id)
		}
	}
	c.status = task.Status_STOPPED
	c.exit = uint32(ret)
	c.exitTime = timeStamp
	for _, exec := range c.execs {
		exec.markExited(uint32(ret))
	}

	log.Debugf("The container %s status is StatusStopped.", c.id)
	s.mu.Unlock()

	go func(ts time.Time, cid string, status int) {
		s.ec <- exit{
			ts:     ts,
			cid:    cid,
			execid: "",
			pid:    shimPid,
			status: status,
		}
	}(timeStamp, c.id, int(ret))

	return int32(ret), nil
}

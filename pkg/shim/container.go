// Package shim provides the implementation of the containerd shim v2 interface for micran.
package shim

import (
	"context"
	"errors"
	"fmt"
	"io"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/micantainer"
	"net/url"
	"os"
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
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	exitCh      chan uint32
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
		exitCh:      make(chan uint32, 1),
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
	}

	return c, nil
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
		log.Debugf("************ binary io ************")
		ioImpl, err = newBinaryIO(ctx, id, uri)
	case "file":
		log.Debugf("************ file io ************")
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
	log.Debugf("<== io stream: %v", p.in)
	return p.in
}

func (p *pipeIO) Stdout() io.Writer {
	log.Debugf("=> io stream: %v", p.out)
	return p.out
}

func (p *pipeIO) Stderr() io.Writer {
	log.Debugf("=> io stream: %v", p.out)
	return p.out
}

func (b *binaryIO) Close() error {
	log.Debugf("=> io stream: %v, %v", b.cmd, b.out)
	err0 := b.out.Close()
	err1 := b.cmd.Cancel()
	return errors.Join(err0, err1)
}

func (b *binaryIO) Stdin() io.ReadCloser {
	return nil
}

func (b *binaryIO) Stdout() io.Writer {
	log.Debugf("=> io stream: %v", b.out)
	return b.out.w
}

func (b *binaryIO) Stderr() io.Writer {
	log.Debugf("=> io stream: %v", b.out)
	return b.out.w
}

func (f *fileIO) Close() error {
	log.Debugf("io stream, close file: %v", f.in)
	var err error
	if err = f.out.Close(); err != nil && f.out != nil {
		return err
	}
	return nil
}

func (f *fileIO) Stdin() io.ReadCloser {
	log.Debugf("<== io stream, open file: %v", f.in)
	return nil
}

func (f *fileIO) Stdout() io.Writer {
	log.Debugf("=> io stream, open file: %v", f.out)
	return f.out
}

func (f *fileIO) Stderr() io.Writer {
	log.Debugf("=> io stream, open file: %v", f.out)
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

	wg.Wait()
	close(exitch)
	log.Debug("All IO copies completed.")
}

// waitContainerExit waits for the container to exit and updates its status.
func waitContainerExit(ctx context.Context, s *shimService, c *container) (int32, error) {
	// Wait for IO streams to close, or mock an exit after a timeout since micad
	// cannot yet detect client OS exit.
	const mockExitTimeout = 5 * time.Second
	select {
	case <-c.exitIOch:
		log.WithField("container", c.id).Debug("The container IO streams closed.")
	case <-time.After(mockExitTimeout):
		log.WithField("container", c.id).Infof("No IO activity; mock exit after %s.", mockExitTimeout)
	}

    // Ask the sandbox to wait for container (container:task = 1:1) exit (non-destructive path).
	ret, err := s.sandbox.WaitContainerExit(ctx, c.id)
	if err != nil && ret == okExitCode {
			ret = Exit255
	}

	timeStamp := time.Now()

	s.mu.Lock()
	// Update container status and exit information.
	if c.cType.CanBeSandbox() {
		if s.monitor != nil {
			s.monitor <- nil
		}

		if err = s.sandbox.Stop(ctx, true); err != nil {
			log.Errorf("Failed to stop sandbox %s.", s.sandbox.SandboxID())
		}

		if err = s.sandbox.Delete(ctx); err != nil {
			log.Errorf("Failed to delete sandbox %s.", s.sandbox.SandboxID())
		}
	} else {
		if _, err := s.sandbox.StopContainer(ctx, c.id, true); err != nil {
			log.Errorf("Failed to stop pod container %s.", c.id)
		}
	}
	c.status = task.Status_STOPPED
	c.exit = uint32(ret)
	c.exitTime = timeStamp

	c.exitCh <- uint32(ret)
	log.Debugf("The container %s status is StatusStopped.", c.id)
	s.mu.Unlock()

	go func() {
		s.ec <- exit{
			ts:     timeStamp,
			cid:    c.id,
			execid: "",
			pid:    shimPid,
			status: int(ret),
		}
	}()

	return int32(ret), nil
}

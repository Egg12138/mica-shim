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
	// stdout, stderr => consoleOut
	stdout string
	stderr string
	bundle string
	cType  cntr.ContainerType
	// exit status code
	exit     uint32
	status   task.Status
	terminal bool
	mounted  bool
}

func newContainer(s *shimService, r *taskAPI.CreateTaskRequest, cType cntr.ContainerType, ocispec *specs.Spec, mounted bool) (*container, error) {
	if r == nil {
		return nil, errdefs.ToGRPCf(errdefs.ErrInvalidArgument, " CreateTaskRequest points to nil")
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

type stdio struct {
	Stdin    string
	Stdout   string
	Stderr   string
	Terminal bool
}

// caller:
// io.Stdout().Write(data)
// io.Close() => close all resources of current stream
type IO interface {
	// close all resources of current stream
	io.Closer
	Stdin() io.ReadCloser
	// temporary: stdout() and stderr() are the same writer for our all io components
	Stdout() io.Writer
	Stderr() io.Writer
}

type ttyIO struct {
	io     IO
	stream *stdio
}

func (tty *ttyIO) close() {
	tty.io.Close()
}

func newTtyIO(ctx context.Context, id, stdin, stdout, stderr string, terminal bool) (*ttyIO, error) {
	// TODO implement
	var err error
	var io IO
	stream := &stdio{
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stderr,
		Terminal: terminal,
	}

	// containerd default io uri is fifo
	uri, err := url.Parse(stdout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse stdout uri: %w", err)
	}

	if uri.Scheme == "" {
		uri.Scheme = "fifo"
	}

	log.Debugf("uri parsed => %+v", uri)
	switch uri.Scheme {
	case "fifo":
		io, err = newPipeIO(ctx, stream)
	case "binary":
		log.Debugf("************ binary io ************")
		io, err = newBinaryIO(ctx, id, uri)
	case "file":
		log.Debugf("************ file io ************")
		io, err = newFileIO(ctx, stream, uri)
	default:
		return nil, fmt.Errorf("unknown STDIO scheme %s", uri.Scheme)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create io stream: %w", err)
	}

	return &ttyIO{
		io:     io,
		stream: stream,
	}, nil
}

var (
	_ IO = &pipeIO{}
	_ IO = &binaryIO{}
	_ IO = &fileIO{}
)

type pipeIO struct {
	in  io.ReadCloser
	out io.WriteCloser
}

// binaryIO related code is from https://github.com/containerd/containerd/blob/v1.6.6/pkg/process/io.go#L311
type binaryIO struct {
	cmd *execabs.Cmd
	out *pipe
}

// fileIO only support write both stdout/stderr to the same file
type fileIO struct {
	out io.WriteCloser
	// file path
	in string
}

func newPipeIO(ctx context.Context, stdio *stdio) (*pipeIO, error) {
	var in io.ReadCloser
	var out io.WriteCloser
	var err error
	if stdio.Stdin != "" {
		fifoFlags := syscall.O_RDONLY | syscall.O_NONBLOCK
		// default perm, let perm set by containerd
		perm := os.FileMode(0)
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

// NewBinaryIO runs a custom binary process for pluggable shim logging
// func NewBinaryIO(ctx context.Context, id string, uri *url.URL) (_ runc.IO, err error) {
//
//	type IO interface {
//		io.Closer
//		Stdin() io.WriteCloser
//		Stdout() io.ReadCloser
//		Stderr() io.ReadCloser
//		Set(*exec.Cmd)
//	}
func newBinaryIO(ctx context.Context, id string, uri *url.URL) (bio *binaryIO, err error) {
	return nil, errdefs.ErrNotImplemented
}

// interface implementations"

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

type pipe struct {
	r *os.File
	w *os.File
}

func (p *pipe) Close() error {
	errw := p.w.Close()
	errr := p.r.Close()
	return errors.Join(errw, errr)
}

// Terminal <-> Containerd <-> shim ::tty.io.Stdin() <-> pipe
func ioCopy(exitch, stdinCloser chan struct{}, tty *ttyIO, stdinPipe io.WriteCloser, stdoutPipe io.Reader) {
	var wg sync.WaitGroup

	if tty.io.Stdin() != nil {
		wg.Add(1)
		go func() {
			log.Debug("Starting stdin copy from containerd to PTY")
			// TALK: maybe CopyBuffer, using a buffer pool is a better choice?
			io.Copy(stdinPipe, tty.io.Stdin())
			log.Debug("Stdin copy completed")
			close(stdinCloser)
			wg.Done()
			log.Info("stdin io stream copy exited")
		}()
	}

	// stdout: client -> pipe -> shim ::tty.io.Stdout() -> containerd -> Terminal
	// stderr: Since RTOS doesn't distinguish stderr, we also copy from PTY stdout
	// This ensures containerd receives the same output on both streams
	if tty.io.Stdout() != nil {
		wg.Add(1)
		go func() {
			log.Debug("Starting stdout copy from PTY to containerd")
			io.Copy(tty.io.Stdout(), stdoutPipe)
			log.Debug("Stdout copy completed")
			wg.Done()
			if tty.io.Stdin() != nil {
				tty.io.Stdin().Close()
			}
			log.Info("out stream copy exited")
		}()
	}

	wg.Wait()
	close(exitch)
	log.Debug("All IO copies completed")
}

func waitContainerExit(ctx context.Context, s *shimService, c *container) (int32, error) {
	// Wait for IO streams to close, or mock an exit after timeout since micad can't detect client OS exit yet
	const mockExitTimeout = 5 * time.Second
	select {
	case <-c.exitIOch:
		log.WithField("container", c.id).Debug("The container IO streams closed")
	case <-time.After(mockExitTimeout):
		log.WithField("container", c.id).Infof("No IO activity; mock exit after %s", mockExitTimeout)
	}

	ret, err := s.sandbox.WaitTaskExit(ctx, c.id, c.id)
	if err != nil {
		if ret == okExitCode {
			ret = Exit255
		}
	}

	timeStamp := time.Now()

	s.mu.Lock()
	// Update container status and exit information
	if c.cType.CanBeSandbox() {
		if s.monitor != nil {
			s.monitor <- nil
		}

		if err = s.sandbox.Stop(ctx, true); err != nil {
			log.Debugf("failed to stop sandbox %s", s.sandbox.SandboxID())
			log.Errorf("failed to stop sandbox %s", s.sandbox.SandboxID())
		}

		if err = s.sandbox.Delete(ctx); err != nil {
			log.Debugf("failed to delete sandbox %s", s.sandbox.SandboxID())
			log.Errorf("failed to delete sandbox %s", s.sandbox.SandboxID())
		}
	} else {
		if _, err := s.sandbox.StopContainer(ctx, c.id, true); err != nil {
			log.Debugf("failed to stop pod container %s", c.id)
			log.Errorf("failed to stop pod container %s", c.id)
		}
	}
	c.status = task.Status_STOPPED
	c.exit = uint32(ret)
	c.exitTime = timeStamp

	c.exitCh <- uint32(ret)
	log.WithField("container", c.id).Debug("The container status is StatusStopped")
	s.mu.Unlock()

	go func() {
		s.ec <- exit{
			ts:     timeStamp,
			cid:    c.id,
			execid: "",
			pid:    s.shimPid,
			status: int(ret),
		}
	}()

	return ret, nil
}

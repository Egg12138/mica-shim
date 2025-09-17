package micantainer

import (
	"context"
	"io"

	er "mica-shim/pkg/errors"
)

type iostream struct {
	sandbox   *Sandbox
	container *Container
	taskId    string
	closed    bool
}

// io.WriteCloser
type stdinStream struct {
	*iostream
}

// io.Reader
// NOTICE: currently, we do not dispatch stderr and stdout together
type stdoutStream struct {
	*iostream
}

// io.Reader
// For future
type stderrStream struct {
	*iostream
}

func newIOStream(s *Sandbox, c *Container, proc string) *iostream {
	return &iostream{
		sandbox:   s,
		container: c,
		taskId:    proc,
		closed:    false, // needed to workaround buggy structcheck
	}
}

func (s *iostream) stdin() io.WriteCloser {
	return &stdinStream{s}
}

func (s *iostream) stdout() io.Reader {
	return &stdoutStream{s}
}

func (s *iostream) stderr() io.Reader {
	return &stderrStream{s}
}

func (s *stdinStream) Write(data []byte) (n int, err error) {
	if s.closed {
		return 0, er.ErrIOClose
	}

	// can not pass context to Write(), so use background context
	return 0, nil
}

func (s *stdinStream) Close() error {
	if s.closed {
		return er.ErrIOClose
	}

	// can not pass context to Close(), so use background context
	err := s.sandbox.resManager.closeTaskStdin(context.Background(), s.container, s.taskId)
	if err == nil {
		s.closed = true
	}

	return err
}

func (s *stdoutStream) Read(data []byte) (n int, err error) {
	if s.closed {
		return 0, er.ErrIOClose
	}

	// can not pass context to Read(), so use background context
	return s.sandbox.resManager.readTaskStdout(context.Background(), s.container, s.taskId, data)
}

func (s *stderrStream) Read(data []byte) (n int, err error) {
	if s.closed {
		return 0, er.ErrIOClose
	}

	// can not pass context to Read(), so use background context
	return s.sandbox.resManager.readTaskStdout(context.Background(), s.container, s.taskId, data)
}

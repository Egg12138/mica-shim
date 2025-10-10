package micantainer

import (
	"fmt"
	"io"
	"os"
	"time"

	defs "mica-shim/definitions"
	er "mica-shim/errors"
	libmica "mica-shim/pkg/libmica"
)

type iostream struct {
	sandbox   *Sandbox
	container *Container
	taskId    string
	closed    bool
	pty       *os.File
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

// BUG: mica create ttydevice not by container id
func (s *iostream) ensureDevice() error {
	if s.container != nil && s.container.config != nil && s.container.config.Infra {
		return nil
	}
	if defs.IsMock {
		return nil
	}
	if s.pty != nil {
		return nil
	}
	// naive discovery: first existing /dev/ttyRPMSG%d
	deadline := time.Now().Add(5 * time.Second)
	for {
		for i := 0; i < libmica.MaxPTYDevices; i++ {
			path := fmt.Sprintf(libmica.PTYDevicePattern, i)
			fi, err := os.Stat(path)
			if err != nil {
				continue
			}
			if fi.Mode()&os.ModeCharDevice == 0 {
				continue
			}
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				continue
			}
			s.pty = f
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("pty device not found")
		}
		time.Sleep(100 * time.Millisecond)
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

	if s.container != nil && s.container.config != nil && s.container.config.Infra {
		// drop stdin for infra containers
		return len(data), nil
	}
	if defs.IsMock {
		return len(data), nil
	}
	if err := s.ensureDevice(); err != nil {
		return 0, err
	}
	return s.pty.Write(data)
}

func (s *stdinStream) Close() error {
	if s.closed {
		return er.ErrIOClose
	}

	err := s.sandbox.resManager.closeTaskStdin(s.container, s.taskId)
	if err == nil {
		s.closed = true
	}

	return err
}

func (s *stdoutStream) Read(data []byte) (n int, err error) {
	if s.closed {
		return 0, er.ErrIOClose
	}
	if s.container != nil && s.container.config != nil && s.container.config.Infra {
		// EOF immediately
		return 0, io.EOF
	}
	if defs.IsMock {
		return 0, io.EOF
	}
	if err := s.ensureDevice(); err != nil {
		return 0, err
	}
	return s.pty.Read(data)
}

func (s *stderrStream) Read(data []byte) (n int, err error) {
	if s.closed {
		return 0, er.ErrIOClose
	}

	// same as stdout for now
	return (&stdoutStream{s.iostream}).Read(data)
}

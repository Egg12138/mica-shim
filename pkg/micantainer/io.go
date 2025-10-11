package micantainer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	defs "mica-shim/definitions"
	er "mica-shim/errors"
	log "mica-shim/logger"
	"mica-shim/pkg/libmica"
	utils "mica-shim/pkg/utils"
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
	// Highest priority: explicit override for debugging/testing.
	if override := os.Getenv("MICRAN_PTY_DEVICE"); override != "" {
		f, err := os.OpenFile(override, os.O_RDWR, 0)
		if err == nil {
			s.pty = f
			return nil
		}
	}

	shortID := ""
	if s.container != nil {
		shortID = utils.ShortID(s.container.id)
	}

	if shortID == "" {
		return er.EmptyContainerID
	}

	// Prefer client-name based symlink provided by micad (ttyRPMSG_<name> -> /dev/pts/N).
	symlink := fmt.Sprintf(libmica.PTYDevPattern, shortID)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		target, err := filepath.EvalSymlinks(symlink)
		if err == nil {
			if f, err := os.OpenFile(target, os.O_RDWR, 0); err == nil {
				s.pty = f
				return nil
			}
		} else if !os.IsNotExist(err) {
			log.Infof("wait for pty device %s prepared", target)
		}
		time.Sleep(100 * time.Millisecond)
	}

    // Legacy numeric discovery ONLY in mock mode
    if defs.IsMock {
        deadline := time.Now().Add(2 * time.Second)
        for time.Now().Before(deadline) {
            for i := range libmica.MaxPTYDevLegacyNum {
                path := fmt.Sprintf(libmica.PTYDevLegacyPattern, i)
                if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
                    s.pty = f
                    return nil
                }
            }
            time.Sleep(100 * time.Millisecond)
        }
    }
    return fmt.Errorf("pty device not found for client %s", s.container.id)
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
		return 0, er.IOClosed
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
		return er.IOClosed
	}

	err := s.sandbox.resManager.closeTaskStdin(s.container, s.taskId)
	if err == nil {
		s.closed = true
	}

	return err
}

func (s *stdoutStream) Read(data []byte) (n int, err error) {
	if s.closed {
		return 0, er.IOClosed
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
		return 0, er.IOClosed
	}

	// same as stdout for now
	return (&stdoutStream{s.iostream}).Read(data)
}

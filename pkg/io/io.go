package io

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	log "mica-shim/logger"

	"github.com/containerd/fifo"
)

// PipeIO copies data from an anonymous pipe to a named pipe
type PipeIO struct {
	p   *pipe
	dst string
}

// NewPipeIO creates an anonymous pipe for copying data to dst named pipe
func NewPipeIO(dst string) (*PipeIO, error) {
	log.Debugf("PipeIO: creating new PipeIO for destination %s", dst)
	p, err := newPipe()
	if err != nil {
		log.Errorf("PipeIO: failed to create pipe for %s: %v", dst, err)
		return nil, fmt.Errorf("creating pipe: %w", err)
	}

	log.Debugf("PipeIO: successfully created PipeIO for destination %s", dst)
	return &PipeIO{
		p:   p,
		dst: dst,
	}, nil
}

// Copy copies data from anonymous pipe to dst pipe until closed
func (pio *PipeIO) Copy(ctx context.Context) error {
	log.Debugf("PipeIO: starting copy operation for destination %s", pio.dst)

	ok, err := fifo.IsFifo(pio.dst)
	if err != nil {
		log.Errorf("PipeIO: error checking fifo %s: %v", pio.dst, err)
		return fmt.Errorf("checking whether file %s is a fifo: %w", pio.dst, err)
	}
	if !ok {
		log.Errorf("PipeIO: target %s is not a fifo", pio.dst)
		return fmt.Errorf("file %s is not a fifo", pio.dst)
	}

	log.Debugf("PipeIO: confirmed %s is a valid FIFO", pio.dst)

	var fw io.WriteCloser
	var fr io.Closer

	log.Debugf("PipeIO: opening write-only FIFO %s", pio.dst)
	if fw, err = fifo.OpenFifo(ctx, pio.dst, syscall.O_WRONLY, 0); err != nil {
		log.Errorf("PipeIO: failed to open write-only FIFO %s: %v", pio.dst, err)
		return fmt.Errorf("opening write only fifo %s: %w", pio.dst, err)
	}
	defer fw.Close()
	log.Debugf("PipeIO: write-only FIFO %s opened", pio.dst)

	// Keep read end open to avoid "broken pipe" in detached mode
	log.Debugf("PipeIO: opening read-only FIFO %s to avoid broken pipe", pio.dst)
	if fr, err = fifo.OpenFifo(ctx, pio.dst, syscall.O_RDONLY, 0); err != nil {
		log.Errorf("PipeIO: failed to open read-only FIFO %s: %v", pio.dst, err)
		return fmt.Errorf("opening read only fifo %s: %w", pio.dst, err)
	}
	defer fr.Close()
	log.Debugf("PipeIO: read-only FIFO %s opened", pio.dst)

	log.Debugf("PipeIO: starting data copy to FIFO %s", pio.dst)
	b := make([]byte, 4096)

	// Custom copy with debug output
	totalBytes := int64(0)
	for {
		select {
		case <-ctx.Done():
			log.Debugf("PipeIO: copy operation cancelled by context for %s after %d bytes", pio.dst, totalBytes)
			return ctx.Err()
		default:
			n, readErr := pio.p.r.Read(b)
			if n > 0 {
				data := b[:n]
				dataStr := string(data)
				log.Debugf("PipeIO: read %d bytes from anonymous pipe for %s: %q", n, pio.dst, dataStr)

				// Special debug for Hello Zephyr
				if containsHelloZephyr(dataStr) {
					log.Debugf("PipeIO: detected Hello Zephyr marker for %s: %q", pio.dst, dataStr)
				}

				written, writeErr := fw.Write(data)
				if writeErr != nil {
					if isPipeClosed(writeErr) {
						log.Debugf("PipeIO: destination %s closed, stopping copy after %d bytes", pio.dst, totalBytes)
						return nil
					}
					log.Errorf("PipeIO: write error for %s after %d bytes: %v", pio.dst, totalBytes, writeErr)
					return fmt.Errorf("copying pipe data to destination: %w", writeErr)
				}
				totalBytes += int64(written)
				log.Debugf("PipeIO: wrote %d bytes to FIFO %s (total %d)", written, pio.dst, totalBytes)
			}

			if readErr != nil {
				if readErr == io.EOF {
					log.Debugf("PipeIO: copy completed for %s after %d bytes", pio.dst, totalBytes)
					return nil
				}
				if isPipeClosed(readErr) {
					log.Debugf("PipeIO: pipe closed for %s after %d bytes", pio.dst, totalBytes)
					return nil
				}
				log.Errorf("PipeIO: read error for %s after %d bytes: %v", pio.dst, totalBytes, readErr)
				return fmt.Errorf("copying pipe data to destination: %w", readErr)
			}
		}
	}
}

// containsHelloZephyr checks if the data contains Hello or Zephyr
func containsHelloZephyr(data string) bool {
	return contains(data, "Hello") || contains(data, "Zephyr")
}

// Simple contains function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Writer returns a writer to the anonymous pipe
func (pio *PipeIO) Writer() io.Writer {
	log.Debugf("PipeIO: returning writer for destination %s", pio.dst)
	return pio.p.w
}

// Close closes the anonymous pipe
func (pio *PipeIO) Close() error {
	log.Debugf("PipeIO: closing pipe for destination %s", pio.dst)
	err := pio.p.Close()
	if err != nil {
		log.Errorf("PipeIO: error closing pipe for %s: %v", pio.dst, err)
	} else {
		log.Debugf("PipeIO: closed pipe for %s", pio.dst)
	}
	return err
}

// pipe is a connected pair of files (anonymous pipe)
type pipe struct {
	r *os.File
	w *os.File
}

// newPipe creates a pipe
func newPipe() (*pipe, error) {
	log.Debugf("PipeIO: creating new anonymous pipe")
	r, w, err := os.Pipe()
	if err != nil {
		log.Errorf("PipeIO: failed to create OS pipe: %v", err)
		return nil, fmt.Errorf("creating os pipe: %w", err)
	}

	log.Debugf("PipeIO: created anonymous pipe (r=%p, w=%p)", r, w)
	return &pipe{
		r: r,
		w: w,
	}, nil
}

// Close closes both ends of the pipe
func (p *pipe) Close() error {
	log.Debugf("PipeIO: closing anonymous pipe (r=%p, w=%p)", p.r, p.w)
	err := errors.Join(p.w.Close(), p.r.Close())
	if err != nil {
		log.Errorf("PipeIO: error closing anonymous pipe: %v", err)
	} else {
		log.Debugf("PipeIO: closed anonymous pipe")
	}
	return err
}

func isPipeClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, os.ErrClosed) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if errno == syscall.EPIPE || errno == syscall.EIO || errno == syscall.EBADF {
			return true
		}
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return isPipeClosed(pathErr.Err)
	}
	return false
}

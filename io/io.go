package io

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/containerd/fifo"
)

// PipeIO copies data from an anonymous pipe to a named pipe
type PipeIO struct {
	p   *pipe
	dst string
}

// NewPipeIO creates an anonymous pipe for copying data to dst named pipe
func NewPipeIO(dst string) (*PipeIO, error) {
	p, err := newPipe()
	if err != nil {
		return nil, fmt.Errorf("creating pipe: %w", err)
	}

	return &PipeIO{
		p:   p,
		dst: dst,
	}, nil
}

// Copy copies data from anonymous pipe to dst pipe until closed
func (pio *PipeIO) Copy(ctx context.Context) error {
	ok, err := fifo.IsFifo(pio.dst)
	if err != nil {
		return fmt.Errorf("checking whether file %s is a fifo: %w", pio.dst, err)
	}
	if !ok {
		return fmt.Errorf("file %s is not a fifo", pio.dst)
	}

	var fw io.WriteCloser
	var fr io.Closer

	if fw, err = fifo.OpenFifo(ctx, pio.dst, syscall.O_WRONLY, 0); err != nil {
		return fmt.Errorf("opening write only fifo %s: %w", pio.dst, err)
	}
	defer fw.Close()

	// Keep read end open to avoid "broken pipe" in detached mode
	if fr, err = fifo.OpenFifo(ctx, pio.dst, syscall.O_RDONLY, 0); err != nil {
		return fmt.Errorf("opening read only fifo %s: %w", pio.dst, err)
	}
	defer fr.Close()

	b := make([]byte, 4096)
	
	// Custom copy with debug output
	totalBytes := int64(0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			n, readErr := pio.p.r.Read(b)
			if n > 0 {
				data := b[:n]
				
				written, writeErr := fw.Write(data)
				if writeErr != nil {
					return fmt.Errorf("copying pipe data to destination: %w", writeErr)
				}
				totalBytes += int64(written)
			}
			
			if readErr != nil {
				if readErr == io.EOF {
					return nil
				}
				return fmt.Errorf("copying pipe data to destination: %w", readErr)
			}
		}
	}
}

// Writer returns a writer to the anonymous pipe
func (pio *PipeIO) Writer() io.Writer {
	return pio.p.w
}

// Close closes the anonymous pipe
func (pio *PipeIO) Close() error {
	err := pio.p.Close()
	return err
}

// pipe is a connected pair of files (anonymous pipe)
type pipe struct {
	r *os.File
	w *os.File
}

// newPipe creates a pipe
func newPipe() (*pipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("creating os pipe: %w", err)
	}

	return &pipe{
		r: r,
		w: w,
	}, nil
}

// Close closes both ends of the pipe
func (p *pipe) Close() error {
	err := errors.Join(p.w.Close(), p.r.Close())
	return err
}

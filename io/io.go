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
	fmt.Printf("DEBUG: Creating new PipeIO for destination: %s\n", dst)
	p, err := newPipe()
	if err != nil {
		fmt.Printf("DEBUG: Failed to create pipe for %s: %v\n", dst, err)
		return nil, fmt.Errorf("creating pipe: %w", err)
	}

	fmt.Printf("DEBUG: Successfully created PipeIO for destination: %s\n", dst)
	return &PipeIO{
		p:   p,
		dst: dst,
	}, nil
}

// Copy copies data from anonymous pipe to dst pipe until closed
func (pio *PipeIO) Copy(ctx context.Context) error {
	fmt.Printf("DEBUG: Starting Copy operation for destination: %s\n", pio.dst)
	
	ok, err := fifo.IsFifo(pio.dst)
	if err != nil {
		fmt.Printf("DEBUG: Error checking if %s is a fifo: %v\n", pio.dst, err)
		return fmt.Errorf("checking whether file %s is a fifo: %w", pio.dst, err)
	}
	if !ok {
		fmt.Printf("DEBUG: File %s is not a fifo\n", pio.dst)
		return fmt.Errorf("file %s is not a fifo", pio.dst)
	}

	fmt.Printf("DEBUG: Confirmed %s is a valid FIFO\n", pio.dst)

	var fw io.WriteCloser
	var fr io.Closer

	fmt.Printf("DEBUG: Opening write-only FIFO: %s\n", pio.dst)
	if fw, err = fifo.OpenFifo(ctx, pio.dst, syscall.O_WRONLY, 0); err != nil {
		fmt.Printf("DEBUG: Failed to open write-only FIFO %s: %v\n", pio.dst, err)
		return fmt.Errorf("opening write only fifo %s: %w", pio.dst, err)
	}
	defer fw.Close()
	fmt.Printf("DEBUG: Successfully opened write-only FIFO: %s\n", pio.dst)

	// Keep read end open to avoid "broken pipe" in detached mode
	fmt.Printf("DEBUG: Opening read-only FIFO: %s (to avoid broken pipe)\n", pio.dst)
	if fr, err = fifo.OpenFifo(ctx, pio.dst, syscall.O_RDONLY, 0); err != nil {
		fmt.Printf("DEBUG: Failed to open read-only FIFO %s: %v\n", pio.dst, err)
		return fmt.Errorf("opening read only fifo %s: %w", pio.dst, err)
	}
	defer fr.Close()
	fmt.Printf("DEBUG: Successfully opened read-only FIFO: %s\n", pio.dst)

	fmt.Printf("DEBUG: Starting data copy from anonymous pipe to FIFO: %s\n", pio.dst)
	b := make([]byte, 4096)
	
	// Custom copy with debug output
	totalBytes := int64(0)
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("DEBUG: Copy operation cancelled by context for %s, total bytes copied: %d\n", pio.dst, totalBytes)
			return ctx.Err()
		default:
			n, readErr := pio.p.r.Read(b)
			if n > 0 {
				data := b[:n]
				dataStr := string(data)
				fmt.Printf("DEBUG: *** PipeIO Copy: Read %d bytes from anonymous pipe for %s: %q\n", n, pio.dst, dataStr)
				
				// Special debug for Hello Zephyr
				if containsHelloZephyr(dataStr) {
					fmt.Printf("DEBUG: *** HELLO ZEPHYR DETECTED in PipeIO for %s: %q\n", pio.dst, dataStr)
				} else {
					fmt.Printf("DEBUG: *** PipeIO Copy: Read %d bytes from anonymous pipe for %s: %q\n", n, pio.dst, dataStr)
				}

				written, writeErr := fw.Write(data)
				if writeErr != nil {
					fmt.Printf("DEBUG: Write error for %s after %d bytes: %v\n", pio.dst, totalBytes, writeErr)
					return fmt.Errorf("copying pipe data to destination: %w", writeErr)
				}
				totalBytes += int64(written)
				fmt.Printf("DEBUG: *** PipeIO Copy: Wrote %d bytes to FIFO %s, total: %d\n", written, pio.dst, totalBytes)
			}
			
			if readErr != nil {
				if readErr == io.EOF {
					fmt.Printf("DEBUG: Copy completed for %s, total bytes: %d\n", pio.dst, totalBytes)
					return nil
				}
				fmt.Printf("DEBUG: Read error for %s after %d bytes: %v\n", pio.dst, totalBytes, readErr)
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
	fmt.Printf("DEBUG: Returning writer for PipeIO destination: %s\n", pio.dst)
	return pio.p.w
}

// Close closes the anonymous pipe
func (pio *PipeIO) Close() error {
	fmt.Printf("DEBUG: Closing PipeIO for destination: %s\n", pio.dst)
	err := pio.p.Close()
	if err != nil {
		fmt.Printf("DEBUG: Error closing PipeIO for %s: %v\n", pio.dst, err)
	} else {
		fmt.Printf("DEBUG: Successfully closed PipeIO for %s\n", pio.dst)
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
	fmt.Printf("DEBUG: Creating new anonymous pipe\n")
	r, w, err := os.Pipe()
	if err != nil {
		fmt.Printf("DEBUG: Failed to create OS pipe: %v\n", err)
		return nil, fmt.Errorf("creating os pipe: %w", err)
	}

	fmt.Printf("DEBUG: Successfully created anonymous pipe (r=%p, w=%p)\n", r, w)
	return &pipe{
		r: r,
		w: w,
	}, nil
}

// Close closes both ends of the pipe
func (p *pipe) Close() error {
	fmt.Printf("DEBUG: Closing both ends of anonymous pipe (r=%p, w=%p)\n", p.r, p.w)
	err := errors.Join(p.w.Close(), p.r.Close())
	if err != nil {
		fmt.Printf("DEBUG: Error closing anonymous pipe: %v\n", err)
	} else {
		fmt.Printf("DEBUG: Successfully closed anonymous pipe\n")
	}
	return err
}

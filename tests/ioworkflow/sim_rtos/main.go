package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// resolveFDPath returns the filesystem path of an open file descriptor via /proc/self/fd.
func resolveFDPath(f *os.File) (string, error) {
	fd := f.Fd()
	link := fmt.Sprintf("/proc/self/fd/%d", fd)
	target, err := os.Readlink(link)
	if err != nil {
		return "", fmt.Errorf("readlink %s: %w", link, err)
	}
	return target, nil
}

func main() {
	var (
		linkPath   string
		echoBinary bool
	)

	flag.StringVar(&linkPath, "link-path", "/tmp/ttyRPMSG2", "Symlink path to expose the PTY slave as ttyRPMSG2 (no root required)")
	flag.BoolVar(&echoBinary, "binary", false, "Echo raw bytes (binary-safe). If false, line-oriented echo with simple terminal behavior.")
	flag.Parse()

	// Create PTY pair
	master, slave, err := pty.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open pty: %v\n", err)
		os.Exit(1)
	}
	defer master.Close()
	defer slave.Close()

	// Resolve slave path (e.g., /dev/pts/N)
	slavePath, err := resolveFDPath(slave)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve slave path: %v\n", err)
		os.Exit(1)
	}

	// Create/update symlink for quick binding
	_ = os.Remove(linkPath)
	if err := os.Symlink(slavePath, linkPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create symlink %s -> %s: %v\n", linkPath, slavePath, err)
		// Continue anyway; users can point MICRAN_PTY_DEVICE directly to slavePath
	}

	fmt.Printf("SIM_RTOS READY\n")
	fmt.Printf("SLAVE_PATH=%s\n", slavePath)
	fmt.Printf("LINK_PATH=%s\n", linkPath)
	fmt.Printf("HINT: export MICRAN_PTY_DEVICE=%s (or %s)\n", linkPath, slavePath)

	// Handle SIGINT/SIGTERM for graceful exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "signal received, exiting\n")
		master.Close()
		os.Exit(0)
	}()

	// Emit a welcome banner like a minimal RTOS console
	welcome := "RTOS SIM: terminal ready. Commands: PING -> PONG, RESIZE H W -> ack, BIG N -> emit N KB.\n"
	_, _ = master.Write([]byte(welcome))

	if echoBinary {
		// Simple binary echo loop
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				_, _ = master.Write(buf[:n])
			}
			if err != nil {
				time.Sleep(50 * time.Millisecond)
			}
		}
	} else {
		// Line-oriented echo with simple control handling
		reader := bufio.NewReader(master)
		lineBuf := make([]rune, 0, 1024)

		for {
			r, _, err := reader.ReadRune()
			if err != nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}

			switch r {
			case '\r':
				// Normalize CR to LF; echo newline
				_, _ = master.Write([]byte("\r\n"))
				line := string(lineBuf)
				processLine(master, line)
				lineBuf = lineBuf[:0]
			case '\n':
				_, _ = master.Write([]byte("\r\n"))
				line := string(lineBuf)
				processLine(master, line)
				lineBuf = lineBuf[:0]
			case 0x08, 0x7f: // backspace/delete
				if len(lineBuf) > 0 {
					lineBuf = lineBuf[:len(lineBuf)-1]
					// erase on terminal: backspace, space, backspace
					_, _ = master.Write([]byte{0x08, ' ', 0x08})
				}
			default:
				// Echo character and append
				_, _ = master.Write([]byte(string(r)))
				lineBuf = append(lineBuf, r)
			}
		}
	}
}

// processLine handles simple commands and echo behavior.
func processLine(w *os.File, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	upper := strings.ToUpper(line)
	switch {
	case upper == "PING":
		_, _ = w.Write([]byte(fmt.Sprintf("PONG %d\r\n", time.Now().UnixNano())))
	case strings.HasPrefix(upper, "RESIZE "):
		parts := strings.Fields(line)
		if len(parts) == 3 {
			h := parts[1]
			wid := parts[2]
			// validate ints
			if _, err1 := strconv.Atoi(h); err1 == nil {
				if _, err2 := strconv.Atoi(wid); err2 == nil {
					_, _ = w.Write([]byte(fmt.Sprintf("RESIZED %s %s\r\n", h, wid)))
					return
				}
			}
		}
		_, _ = w.Write([]byte("RESIZE INVALID\r\n"))
	case strings.HasPrefix(upper, "BIG "):
		parts := strings.Fields(line)
		if len(parts) == 2 {
			kb, err := strconv.Atoi(parts[1])
			if err == nil && kb > 0 {
				chunk := strings.Repeat("A", 1024)
				for i := 0; i < kb; i++ {
					_, _ = w.Write([]byte(chunk))
				}
				_, _ = w.Write([]byte("\r\n"))
				return
			}
		}
		_, _ = w.Write([]byte("BIG INVALID\r\n"))
	default:
		// Default echo line
		_, _ = w.Write([]byte("ECHO: " + line + "\r\n"))
	}
}

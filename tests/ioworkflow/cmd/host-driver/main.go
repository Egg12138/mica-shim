package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	log "mica-shim/logger"
	libmica "mica-shim/pkg/libmica"

	"github.com/containerd/fifo"
)

func main() {
	var (
		taskID    string
		ptyPath   string
		stdinFifo string
		stdoutFifo string
		stderrFifo string
		terminal  bool
	)

	flag.StringVar(&taskID, "taskid", fmt.Sprintf("iowf-%d", time.Now().Unix()), "Task ID for MicaIO instance")
	flag.StringVar(&ptyPath, "pty", "", "Override PTY device path (e.g., /dev/ttyRPMSG0 or /dev/pts/N). If set, MICRAN_PTY_DEVICE env will be used.")
	flag.StringVar(&stdinFifo, "stdin", "", "Path to stdin FIFO (will be created if not exists). If empty, a temp path will be used.")
	flag.StringVar(&stdoutFifo, "stdout", "", "Path to stdout FIFO (will be created if not exists). If empty, a temp path will be used.")
	flag.StringVar(&stderrFifo, "stderr", "", "Path to stderr FIFO (will be created if not exists). If empty, a temp path will be used (only used when --terminal=false).")
	flag.BoolVar(&terminal, "terminal", true, "Terminal mode. When true, stderr is combined with stdout.")
	flag.Parse()

	if ptyPath != "" {
		// Use environment for override in libmica.MicaIO
		_ = os.Setenv("MICRAN_PTY_DEVICE", ptyPath)
		log.Infof("MICRAN_PTY_DEVICE set to %s", ptyPath)
	}

	tmpBase := filepath.Join(os.TempDir(), fmt.Sprintf("micran-iowf-%d", os.Getpid()))
	if err := os.MkdirAll(tmpBase, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp base dir: %v\n", err)
		os.Exit(1)
	}

	// Default FIFO paths if not provided
	if stdinFifo == "" {
		stdinFifo = filepath.Join(tmpBase, "stdin.fifo")
	}
	if stdoutFifo == "" {
		stdoutFifo = filepath.Join(tmpBase, "stdout.fifo")
	}
	if !terminal && stderrFifo == "" {
		stderrFifo = filepath.Join(tmpBase, "stderr.fifo")
	}

	// Create FIFOs
	if err := ensureFifo(stdinFifo); err != nil {
		fmt.Fprintf(os.Stderr, "failed to ensure stdin fifo: %v\n", err)
		os.Exit(1)
	}
	if err := ensureFifo(stdoutFifo); err != nil {
		fmt.Fprintf(os.Stderr, "failed to ensure stdout fifo: %v\n", err)
		os.Exit(1)
	}
	if !terminal {
		if err := ensureFifo(stderrFifo); err != nil {
			fmt.Fprintf(os.Stderr, "failed to ensure stderr fifo: %v\n", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl-C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Infof("signal received, shutting down")
		cancel()
	}()

	// Start readers to consume stdout/stderr FIFOs and print to console
	var stdoutDone chan struct{}
	var stderrDone chan struct{}
	if os.Getenv("IWF_NO_LOCAL_OUTPUT") != "1" {
		stdoutDone = make(chan struct{})
		go func() {
			defer close(stdoutDone)
			if err := readFifoToWriter(ctx, stdoutFifo, os.Stdout, "STDOUT"); err != nil && err != context.Canceled {
				log.Warnf("stdout reader exited with error: %v", err)
			}
		}()
		if !terminal {
			stderrDone = make(chan struct{})
			go func() {
				defer close(stderrDone)
				if err := readFifoToWriter(ctx, stderrFifo, os.Stdout, "STDERR"); err != nil && err != context.Canceled {
					log.Warnf("stderr reader exited with error: %v", err)
				}
			}()
		}
	} else {
		log.Infof("IWF_NO_LOCAL_OUTPUT=1: skipping local stdout/stderr readers; use the FIFOs to consume output")
	}

	// Create MicaIO
	mio, err := libmica.NewMicaIO(ctx, taskID, stdinFifo, stdoutFifo, stderrFifo, terminal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create MicaIO: %v\n", err)
		os.Exit(1)
	}
	defer mio.Close()

	if err := mio.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start MicaIO: %v\n", err)
		os.Exit(1)
	}
	log.Infof("MicaIO started. PTY: %s | stdin: %s | stdout: %s | stderr: %s | terminal=%v",
		mio.GetPTYDevice(), stdinFifo, stdoutFifo, stderrFifo, terminal)

	// Open stdin FIFO writer to send input to PTY
	stdinWriter, err := fifo.OpenFifo(ctx, stdinFifo, syscall.O_WRONLY, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open stdin fifo for writing: %v\n", err)
		os.Exit(1)
	}
	defer stdinWriter.Close()

	// Simple REPL: read lines from stdin and forward to PTY via stdin FIFO
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Host-driver ready. Type lines to send to PTY.\n")
	fmt.Printf("Commands:\n")
	fmt.Printf("  :quit                -> exit\n")
	fmt.Printf("  :resize H W          -> send resize control line (RESIZE H W) to PTY\n")
	fmt.Printf("  :big N               -> send N KB of 'A' to PTY\n")
	fmt.Printf("  :hex HEXBYTES        -> send raw bytes from hex (e.g., :hex 00ff0a)\n")
	fmt.Printf("\n")

	for {
		select {
		case <-ctx.Done():
			log.Infof("context done, exiting REPL")
			return
		default:
			fmt.Print("> ")
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					log.Infof("stdin EOF; continuing without interactive input. Use FIFOs for IO (stdin: %s, stdout: %s).", stdinFifo, stdoutFifo)
					<-ctx.Done()
					log.Infof("context done, exiting")
					return
				}
				log.Warnf("stdin read error: %v", err)
				continue
			}
			line = strings.TrimRight(line, "\r\n")

			// Commands
			if strings.HasPrefix(line, ":quit") {
				log.Infof("quit command received")
				return
			}
			if strings.HasPrefix(line, ":resize ") {
				args := strings.Fields(line)
				if len(args) == 3 {
					payload := fmt.Sprintf("RESIZE %s %s\n", args[1], args[2])
					if _, err := io.WriteString(stdinWriter, payload); err != nil {
						log.Warnf("failed to write resize control to stdin fifo: %v", err)
					}
				} else {
					fmt.Println("usage: :resize H W")
				}
				continue
			}
			if strings.HasPrefix(line, ":big ") {
				args := strings.Fields(line)
				if len(args) == 2 {
					kb := parseInt(args[1], 0)
					if kb <= 0 {
						fmt.Println("usage: :big N (N>0)")
						continue
					}
					chunk := strings.Repeat("A", 1024)
					for i := 0; i < kb; i++ {
						if _, err := io.WriteString(stdinWriter, chunk); err != nil {
							log.Warnf("failed to write big chunk: %v", err)
							break
						}
					}
					// Add newline to complete a line in typical shells
					_, _ = io.WriteString(stdinWriter, "\n")
				} else {
					fmt.Println("usage: :big N")
				}
				continue
			}
			if strings.HasPrefix(line, ":hex ") {
				hexstr := strings.TrimSpace(strings.TrimPrefix(line, ":hex"))
				data, err := parseHex(hexstr)
				if err != nil {
					fmt.Printf("invalid hex: %v\n", err)
					continue
				}
				if _, err := stdinWriter.Write(data); err != nil {
					log.Warnf("failed to write hex bytes: %v", err)
				}
				// newline for visibility
				_, _ = io.WriteString(stdinWriter, "\n")
				continue
			}

			// Default: send line with newline
			if _, err := io.WriteString(stdinWriter, line+"\n"); err != nil {
				log.Warnf("failed to write to stdin fifo: %v", err)
			}
		}
	}
}

func ensureFifo(path string) error {
	// If exists, check it's a FIFO
	if st, err := os.Stat(path); err == nil {
		// If not FIFO, error; if FIFO, ok
		if (st.Mode() & os.ModeNamedPipe) == 0 {
			return fmt.Errorf("path exists and is not a FIFO: %s", path)
		}
		return nil
	}
	// Create FIFO
	if err := syscall.Mkfifo(path, 0o666); err != nil {
		return fmt.Errorf("mkfifo %s: %w", path, err)
	}
	return nil
}

func readFifoToWriter(ctx context.Context, fifoPath string, w io.Writer, tag string) error {
	// Open the FIFO for reading (blocking)
	fr, err := fifo.OpenFifo(ctx, fifoPath, syscall.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open fifo for reading %s: %w", fifoPath, err)
	}
	defer fr.Close()

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
			n, err := fr.Read(buf)
			if n > 0 {
				// Write as-is to the writer
				if _, werr := w.Write(buf[:n]); werr != nil {
					return fmt.Errorf("write to %s failed: %w", tag, werr)
				}
			}
			if err != nil {
				if err == io.EOF {
					// Producer closed; wait briefly and try to reopen?
					time.Sleep(100 * time.Millisecond)
					continue
				}
				return fmt.Errorf("read from %s fifo: %w", tag, err)
			}
		}
	}
}

func parseInt(s string, def int) int {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return def
	}
	return n
}

func parseHex(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("hex string length must be even")
	}
	out := make([]byte, 0, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		var b byte
		_, err := fmt.Sscanf(s[i:i+2], "%02x", &b)
		if err != nil {
			return nil, fmt.Errorf("invalid hex at pos %d: %v", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultSocketDir    = "/tmp/mica"
	defaultCreateSocket = defaultSocketDir + "/mica-create.socket"
	ptyPrefix           = "/tmp/ttyRPMSG_"
	defaultTimeout      = 3 * time.Second
	maxIDLength         = 31
)

func main() {
	var (
		containerID string
		socketDir   string
		createSock  string
		forceCreate bool
		timeout     time.Duration
	)

	flag.StringVar(&containerID, "id", "", "Container ID (long or short). Required for control commands and PTY tailing.")
	flag.StringVar(&socketDir, "socket-dir", defaultSocketDir, "Directory containing per-client control sockets.")
	flag.StringVar(&createSock, "create-socket", defaultCreateSocket, "mica-create control socket path (mock mode fallback).")
	flag.BoolVar(&forceCreate, "create", false, "Send control commands via mica-create socket instead of per-client sockets.")
	flag.DurationVar(&timeout, "timeout", defaultTimeout, "Socket read/write timeout.")
    flag.Usage = func() {
        fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] <command>\n", os.Args[0])
        fmt.Fprintln(flag.CommandLine.Output(), "Commands: create | start | stop | status | rm | tail")
        fmt.Fprintln(flag.CommandLine.Output(), "Examples:")
        fmt.Fprintf(flag.CommandLine.Output(), "  %s --id codex-test create\n", os.Args[0])
        fmt.Fprintf(flag.CommandLine.Output(), "  %s --id codex-test start\n", os.Args[0])
        fmt.Fprintf(flag.CommandLine.Output(), "  %s --id codex-test status\n", os.Args[0])
        fmt.Fprintf(flag.CommandLine.Output(), "  %s --id codex-test tail\n", os.Args[0])
        fmt.Fprintln(flag.CommandLine.Output())
        flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	command := strings.ToLower(args[0])
	if command == "tail" && containerID == "" {
		fmt.Fprintln(os.Stderr, "--id is required for tail command")
		os.Exit(2)
	}

    if command != "tail" && command != "start" && command != "stop" && command != "status" && command != "rm" && command != "create" {
        fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
        os.Exit(2)
    }

	shortID := truncateID(containerID)

	switch command {
	case "tail":
		ptyPath := ptyPrefix + shortID
		fmt.Printf(">>> tailing PTY %s (press Ctrl+C to stop)\n", ptyPath)
		if err := tailPTY(ptyPath); err != nil && !errors.Is(err, os.ErrClosed) {
			fmt.Fprintf(os.Stderr, "tail error: %v\n", err)
			os.Exit(1)
		}
		return
    case "create":
        if containerID == "" {
            fmt.Fprintln(os.Stderr, "--id is required for create")
            os.Exit(2)
        }
        // textual create goes to create socket and includes the id
        if err := sendCommand(createSock, "create "+shortID, timeout); err != nil {
            fmt.Fprintf(os.Stderr, "create via %s failed: %v\n", createSock, err)
            os.Exit(1)
        }
        fmt.Printf("[create] %s OK\n", createSock)
        return
    default:
        if containerID == "" {
            fmt.Fprintln(os.Stderr, "--id is required for control commands")
            os.Exit(2)
        }
		path := commandSocketPath(shortID, socketDir, createSock, forceCreate)
		sentPath := path
		if err := sendCommand(path, command, timeout); err != nil {
			// If per-client socket failed and we are not forcing, retry via create socket once.
			if !forceCreate && path != createSock {
				fmt.Fprintf(os.Stderr, "command via %s failed (%v), retrying via %s\n", path, err, createSock)
				if err2 := sendCommand(createSock, command, timeout); err2 != nil {
					fmt.Fprintf(os.Stderr, "command via %s failed: %v\n", createSock, err2)
					os.Exit(1)
				}
				sentPath = createSock
			} else {
				fmt.Fprintf(os.Stderr, "command via %s failed: %v\n", path, err)
				os.Exit(1)
			}
		}
		fmt.Printf("[%s] %s OK\n", command, sentPath)
	}
}

func commandSocketPath(shortID, socketDir, createSock string, force bool) string {
	if force {
		return createSock
	}
	clientSock := filepath.Join(socketDir, shortID+".socket")
	if _, err := os.Stat(clientSock); err == nil {
		return clientSock
	}
	return createSock
}

func truncateID(id string) string {
	if len(id) <= maxIDLength {
		return id
	}
	return id[:maxIDLength]
}

func sendCommand(socketPath, command string, timeout time.Duration) error {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return fmt.Errorf("dial %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	if _, err := conn.Write([]byte(command)); err != nil {
		return fmt.Errorf("write command: %w", err)
	}

	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read response: %w", err)
		}
	}

	resp := strings.TrimSpace(string(buf))
	if resp != "" {
		fmt.Println(resp)
	}
	if strings.Contains(resp, "MICA-FAILED") {
		return fmt.Errorf("command failed: %s", resp)
	}
	return nil
}

func tailPTY(path string) error {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open PTY: %w", err)
	}

	var closeOnce sync.Once
	cleanup := func() {
		closeOnce.Do(func() {
			file.Close()
		})
	}
	defer cleanup()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\n[tail] stopping")
		cleanup()
	}()

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if errors.Is(err, os.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read PTY: %w", err)
		}
	}
}

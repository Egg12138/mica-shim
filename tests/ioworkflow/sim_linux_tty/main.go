package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
		shellPath  string
		shellArgs  string
		termEnv    string
	)

	flag.StringVar(&linkPath, "link-path", "/tmp/ttyRPMSG3", "Symlink path to expose the PTY slave (no root required)")
	flag.StringVar(&shellPath, "shell", "/bin/bash", "Path to shell executable")
	flag.StringVar(&shellArgs, "shell-args", "--norc --noprofile", "Arguments for shell (disable rc files for clean session)")
	flag.StringVar(&termEnv, "term", "xterm-256color", "TERM environment variable")
	flag.Parse()

	// Create PTY pair
	master, slave, err := pty.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open pty: %v\n", err)
		os.Exit(1)
	}
	defer master.Close()
	defer slave.Close()

	// Set PTY to raw mode for proper terminal handling
	// Note: Commented out for now as it might interfere with shell
	/*
	oldState, err := term.MakeRaw(int(master.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to set pty to raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(int(master.Fd()), oldState)
	*/

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

	fmt.Printf("LINUX_TTY READY\n")
	fmt.Printf("SLAVE_PATH=%s\n", slavePath)
	fmt.Printf("LINK_PATH=%s\n", linkPath)
	fmt.Printf("SHELL=%s %s\n", shellPath, shellArgs)
	fmt.Printf("TERM=%s\n", termEnv)
	fmt.Printf("HINT: export MICRAN_PTY_DEVICE=%s (or %s)\n", linkPath, slavePath)
	fmt.Fprintf(os.Stderr, "DEBUG: About to start shell...\n")
	os.Stderr.Sync()

	// Handle SIGINT/SIGTERM for graceful exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "signal received, exiting\n")
		master.Close()
		os.Exit(0)
	}()

	// Prepare shell command
	// Split shellArgs into individual arguments
	shellArgList := []string{}
	if shellArgs != "" {
		// Simple split by space for now (could use shlex for more complex scenarios)
		shellArgList = strings.Split(shellArgs, " ")
	}
	cmd := exec.Command(shellPath, shellArgList...)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		// Setctty: true, // Removed as it causes "inappropriate ioctl for device"
	}

	// Set environment variables
	cmd.Env = append(os.Environ(),
		"TERM="+termEnv,
		"PS1=\\u@\\h:\\w\\$ ",
		"LANG=C",
		"LC_ALL=C",
	)

	// Start shell
	fmt.Fprintf(os.Stderr, "DEBUG: Starting shell with command: %s %v\n", shellPath, shellArgList)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start shell: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "DEBUG: Shell started successfully\n")
	fmt.Fprintf(os.Stderr, "DEBUG: Waiting for shell to exit...\n")
	os.Stderr.Sync()

	// Handle shell exit in background
	go func() {
		err := cmd.Wait()
		fmt.Fprintf(os.Stderr, "shell exited with error: %v\n", err)
		master.Close()
		os.Exit(0)
	}()

	// Simple passthrough loop for any additional monitoring/logging
	fmt.Fprintf(os.Stderr, "DEBUG: Starting read loop...\n")
	os.Stderr.Sync()
	buf := make([]byte, 4096)
	for {
		n, err := master.Read(buf)
		if n > 0 {
			// Could add logging here if needed
			fmt.Fprintf(os.Stderr, "DEBUG: Read %d bytes from shell\n", n)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG: Read error: %v\n", err)
			// Exit on read error (PTY closed)
			time.Sleep(50 * time.Millisecond)
			break
		}
	}
	fmt.Fprintf(os.Stderr, "DEBUG: Exiting main loop\n")
	os.Stderr.Sync()
}
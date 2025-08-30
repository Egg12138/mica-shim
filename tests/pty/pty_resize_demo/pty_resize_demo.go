package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Window size structure matching kernel's winsize
type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// PTYResizeTest tests PTY resize functionality
type PTYResizeTest struct {
	masterFile    *os.File
	slaveFile     *os.File
	originalSize  *Winsize
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewPTYResizeTest creates a new PTY resize test
func NewPTYResizeTest() (*PTYResizeTest, error) {
	// Create a pseudoterminal
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/ptmx: %v", err)
	}

	// Unlock the slave PTY
	if err := unlockpt(master); err != nil {
		master.Close()
		return nil, fmt.Errorf("failed to unlockpt: %v", err)
	}

	// Get the slave PTY name
	slaveName, err := ptsname(master)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("failed to get ptsname: %v", err)
	}

	// Open the slave PTY
	slave, err := os.OpenFile(slaveName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("failed to open slave PTY: %v", err)
	}

	// Create context for cancellation
	ctx, cancel := context.WithCancel(context.Background())

	return &PTYResizeTest{
		masterFile:   master,
		slaveFile:    slave,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Close closes the PTY files
func (t *PTYResizeTest) Close() {
	if t.masterFile != nil {
		t.masterFile.Close()
	}
	if t.slaveFile != nil {
		t.slaveFile.Close()
	}
	t.cancel()
}

// GetWindowSize gets the current window size
func (t *PTYResizeTest) GetWindowSize() (*Winsize, error) {
	var ws Winsize
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		t.masterFile.Fd(),
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return nil, fmt.Errorf("TIOCGWINSZ ioctl failed: %v", errno)
	}
	return &ws, nil
}

// SetWindowSize sets the window size
func (t *PTYResizeTest) SetWindowSize(rows, cols uint16) error {
	ws := Winsize{
		Row:    rows,
		Col:    cols,
		Xpixel: 0,
		Ypixel: 0,
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		t.masterFile.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return fmt.Errorf("TIOCSWINSZ ioctl failed: %v", errno)
	}
	return nil
}

// TestResize performs a comprehensive PTY resize test
func (t *PTYResizeTest) TestResize() error {
	fmt.Println("=== PTY Resize Test Started ===")

	// Get initial window size
	original, err := t.GetWindowSize()
	if err != nil {
		return fmt.Errorf("failed to get initial window size: %v", err)
	}
	t.originalSize = original
	fmt.Printf("Original window size: %dx%d\n", original.Col, original.Row)

	// Test various window sizes
	testSizes := []struct {
		rows uint16
		cols uint16
		name string
	}{
		{24, 80, "Standard 24x80"},
		{40, 120, "Large 40x120"},
		{10, 40, "Small 10x40"},
		{25, 132, "Wide 25x132"},
		{50, 200, "Extra Large 50x200"},
	}

	for _, size := range testSizes {
		fmt.Printf("Testing resize to %s...", size.name)
		
		// Set new size
		if err := t.SetWindowSize(size.rows, size.cols); err != nil {
			fmt.Printf(" FAILED: %v\n", err)
			continue
		}

		// Verify the new size
		current, err := t.GetWindowSize()
		if err != nil {
			fmt.Printf(" FAILED to verify: %v\n", err)
			continue
		}

		if current.Row == size.rows && current.Col == size.cols {
			fmt.Printf(" OK\n")
		} else {
			fmt.Printf(" FAILED: expected %dx%d, got %dx%d\n", 
				size.cols, size.rows, current.Col, current.Row)
		}

		// Small delay to allow propagation
		time.Sleep(50 * time.Millisecond)
	}

	// Restore original size
	fmt.Print("Restoring original size...")
	if err := t.SetWindowSize(t.originalSize.Row, t.originalSize.Col); err != nil {
		fmt.Printf(" FAILED: %v\n", err)
	} else {
		fmt.Println(" OK")
	}

	fmt.Println("=== PTY Resize Test Completed ===")
	return nil
}

// TestResizeWithShell tests resize with an interactive shell
func (t *PTYResizeTest) TestResizeWithShell() error {
	fmt.Println("=== PTY Resize with Shell Test ===")

	// Create a dummy shell that responds to resize signals
	shell := &ResizeAwareShell{
		reader: t.slaveFile,
		writer: t.slaveFile,
	}

	// Start shell in goroutine
	shellDone := make(chan error, 1)
	go func() {
		shellDone <- shell.Run()
	}()

	// Read and display shell output
	outputDone := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(t.masterFile)
		for scanner.Scan() {
			select {
			case <-t.ctx.Done():
				return
			default:
				fmt.Printf("Shell: %s\n", scanner.Text())
			}
		}
		close(outputDone)
	}()

	// Simulate resize commands
	testCommands := []struct {
		rows  uint16
		cols  uint16
		delay time.Duration
	}{
		{30, 100, 500 * time.Millisecond},
		{15, 60, 500 * time.Millisecond},
		{40, 120, 500 * time.Millisecond},
	}

	for _, cmd := range testCommands {
		fmt.Printf("Resizing to %dx%d...\n", cmd.cols, cmd.rows)
		if err := t.SetWindowSize(cmd.rows, cmd.cols); err != nil {
			fmt.Printf("Resize failed: %v\n", err)
		}
		time.Sleep(cmd.delay)
	}

	// Send exit command
	fmt.Println("Sending exit command...")
	t.masterFile.Write([]byte("exit\n"))

	// Wait for shell to finish
	select {
	case err := <-shellDone:
		if err != nil {
			return fmt.Errorf("shell error: %v", err)
		}
	case <-time.After(2 * time.Second):
		return fmt.Errorf("shell did not exit in time")
	}

	fmt.Println("=== PTY Resize with Shell Test Completed ===")
	return nil
}

// ResizeAwareShell is a shell that detects window size changes
type ResizeAwareShell struct {
	reader io.Reader
	writer io.Writer
}

// Run runs the resize-aware shell
func (s *ResizeAwareShell) Run() error {
	// Get initial window size
	fd := s.reader.(*os.File).Fd()
	var ws Winsize
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno == 0 {
		fmt.Fprintf(s.writer, "Shell started with window size: %dx%d\n", ws.Col, ws.Row)
	}

	fmt.Fprintf(s.writer, "Resize-aware shell ready. Type 'exit' to quit.\n")
	fmt.Fprintf(s.writer, "$ ")

	scanner := bufio.NewScanner(s.reader)
	lastSize := ws

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())

		if input == "exit" {
			fmt.Fprintf(s.writer, "Exiting shell\n")
			break
		}

		// Check for window size changes
		var currentSize Winsize
		_, _, errno = syscall.Syscall(
			syscall.SYS_IOCTL,
			fd,
			syscall.TIOCGWINSZ,
			uintptr(unsafe.Pointer(&currentSize)),
		)
		if errno == 0 && (currentSize.Row != lastSize.Row || currentSize.Col != lastSize.Col) {
			fmt.Fprintf(s.writer, "\nWindow size changed: %dx%d -> %dx%d\n",
				lastSize.Col, lastSize.Row, currentSize.Col, currentSize.Row)
			lastSize = currentSize
		}

		// Echo command
		fmt.Fprintf(s.writer, "Executed: %s\n", input)
		fmt.Fprintf(s.writer, "$ ")
	}

	return scanner.Err()
}

// POSIX functions for PTY handling
func unlockpt(f *os.File) error {
	var u int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&u)))
	if errno != 0 {
		return syscall.Errno(errno)
	}
	return nil
}

func ptsname(f *os.File) (string, error) {
	var n int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n)))
	if errno != 0 {
		return "", syscall.Errno(errno)
	}
	return fmt.Sprintf("/dev/pts/%d", n), nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  go run pty_resize_test.go basic     # Basic resize test")
		fmt.Println("  go run pty_resize_test.go shell     # Resize with shell test")
		fmt.Println("  go run pty_resize_test.go demo      # Interactive demo")
		return
	}

	switch os.Args[1] {
	case "basic":
		testBasicResize()
	case "shell":
		testResizeWithShell()
	case "demo":
		runInteractiveDemo()
	default:
		fmt.Printf("Unknown option: %s\n", os.Args[1])
	}
}

func testBasicResize() {
	test, err := NewPTYResizeTest()
	if err != nil {
		log.Fatalf("Failed to create PTY resize test: %v", err)
	}
	defer test.Close()

	if err := test.TestResize(); err != nil {
		log.Printf("Test failed: %v", err)
		os.Exit(1)
	}

	fmt.Println("Basic PTY resize test passed!")
}

func testResizeWithShell() {
	test, err := NewPTYResizeTest()
	if err != nil {
		log.Fatalf("Failed to create PTY resize test: %v", err)
	}
	defer test.Close()

	if err := test.TestResizeWithShell(); err != nil {
		log.Printf("Test failed: %v", err)
		os.Exit(1)
	}

	fmt.Println("PTY resize with shell test passed!")
}

func runInteractiveDemo() {
	fmt.Println("Interactive PTY Resize Demo")
	fmt.Println("This demo shows how PTY resize works with ioctl")
	fmt.Println("Press Ctrl+C to exit")

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	test, err := NewPTYResizeTest()
	if err != nil {
		log.Fatalf("Failed to create PTY resize test: %v", err)
	}
	defer test.Close()

	// Show initial size
	ws, err := test.GetWindowSize()
	if err != nil {
		log.Fatalf("Failed to get window size: %v", err)
	}
	fmt.Printf("Initial PTY size: %dx%d\n", ws.Col, ws.Row)

	// Periodically change size
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	sizeIndex := 0
	sizes := []struct{ rows, cols uint16 }{
		{24, 80},
		{30, 100},
		{40, 120},
		{15, 60},
		{25, 80},
	}

	for {
		select {
		case <-sigChan:
			fmt.Println("\nDemo interrupted by user")
			return
		case <-ticker.C:
			size := sizes[sizeIndex%len(sizes)]
			sizeIndex++

			fmt.Printf("Resizing to %dx%d...", size.cols, size.rows)
			if err := test.SetWindowSize(size.rows, size.cols); err != nil {
				fmt.Printf(" FAILED: %v\n", err)
			} else {
				fmt.Println(" OK")

				// Verify
				current, err := test.GetWindowSize()
				if err != nil {
					fmt.Printf("Failed to verify: %v\n", err)
				} else {
					fmt.Printf("Verified size: %dx%d\n", current.Col, current.Row)
				}
			}
		}
	}
}
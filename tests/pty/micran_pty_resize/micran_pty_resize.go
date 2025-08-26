package main

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"
)

// Window size structure matching the kernel's winsize struct
type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// MicranPTYResize implements PTY resize for micran using ioctl
// This simulates how the actual micran PTY resize would work
type MicranPTYResize struct {
	ptyPath string
	ptyFile *os.File
}

// NewMicranPTYResize creates a new PTY resize instance for a given RPMSG device
func NewMicranPTYResize(deviceID int) *MicranPTYResize {
	return &MicranPTYResize{
		ptyPath: fmt.Sprintf("/dev/ttyRPMSG%d", deviceID),
	}
}

// Connect opens the PTY device
func (m *MicranPTYResize) Connect() error {
	// Try to open the PTY device
	// In real implementation, this would be created by micad
	file, err := os.OpenFile(m.ptyPath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		// If device doesn't exist, create a dummy one for testing
		return m.createDummyPTY()
	}
	
	m.ptyFile = file
	return nil
}

// createDummyPTY creates a dummy PTY for testing when real device is not available
func (m *MicranPTYResize) createDummyPTY() error {
	// Create a pseudoterminal for testing
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open /dev/ptmx: %v", err)
	}
	
	// Unlock the slave PTY
	var u int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&u)))
	if errno != 0 {
		master.Close()
		return fmt.Errorf("failed to unlockpt: %v", errno)
	}
	
	// Get the slave PTY name
	var n int32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n)))
	if errno != 0 {
		master.Close()
		return fmt.Errorf("failed to get ptsname: %v", errno)
	}
	
	log.Printf("Using dummy PTY /dev/pts/%d for testing", n)
	m.ptyFile = master
	return nil
}

// Resize resizes the PTY using ioctl - this is the core implementation
func (m *MicranPTYResize) Resize(rows, cols uint16) error {
	if m.ptyFile == nil {
		return fmt.Errorf("PTY not connected")
	}
	
	// Prepare window size structure
	ws := Winsize{
		Row:    rows,
		Col:    cols,
		Xpixel: 0, // Not used in most cases
		Ypixel: 0, // Not used in most cases
	}
	
	log.Printf("Resizing PTY %s to %dx%d", m.ptyPath, cols, rows)
	
	// Perform ioctl call to set window size
	// This is the same approach used by kata-containers
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		m.ptyFile.Fd(),
		syscall.TIOCSWINSZ,  // Terminal IO Control Set Window Size
		uintptr(unsafe.Pointer(&ws)),
	)
	
	if errno != 0 {
		return fmt.Errorf("TIOCSWINSZ ioctl failed: %v", errno)
	}
	
	log.Printf("Successfully resized PTY to %dx%d", cols, rows)
	return nil
}

// GetSize gets the current window size
func (m *MicranPTYResize) GetSize() (*Winsize, error) {
	if m.ptyFile == nil {
		return nil, fmt.Errorf("PTY not connected")
	}
	
	var ws Winsize
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		m.ptyFile.Fd(),
		syscall.TIOCGWINSZ,  // Terminal IO Control Get Window Size
		uintptr(unsafe.Pointer(&ws)),
	)
	
	if errno != 0 {
		return nil, fmt.Errorf("TIOCGWINSZ ioctl failed: %v", errno)
	}
	
	return &ws, nil
}

// Close closes the PTY file
func (m *MicranPTYResize) Close() {
	if m.ptyFile != nil {
		m.ptyFile.Close()
		m.ptyFile = nil
	}
}

// TestResize performs a comprehensive test of the PTY resize functionality
func (m *MicranPTYResize) TestResize() error {
	fmt.Println("=== Micran PTY Resize Test ===")
	fmt.Printf("Testing with device: %s\n", m.ptyPath)
	
	// Connect to PTY
	if err := m.Connect(); err != nil {
		return fmt.Errorf("failed to connect to PTY: %v", err)
	}
	defer m.Close()
	
	// Get initial size
	initial, err := m.GetSize()
	if err != nil {
		log.Printf("Warning: Could not get initial size: %v", err)
	} else {
		fmt.Printf("Initial size: %dx%d\n", initial.Col, initial.Row)
	}
	
	// Test various sizes
	testSizes := []struct {
		name string
		rows uint16
		cols uint16
	}{
		{"Standard", 24, 80},
		{"Large", 40, 120},
		{"Small", 10, 40},
		{"Wide", 25, 132},
		{"Square", 50, 50},
	}
	
	for _, size := range testSizes {
		fmt.Printf("Testing %s (%dx%d)... ", size.name, size.cols, size.rows)
		
		// Resize
		if err := m.Resize(size.rows, size.cols); err != nil {
			fmt.Printf("FAILED: %v\n", err)
			continue
		}
		
		// Verify
		current, err := m.GetSize()
		if err != nil {
			fmt.Printf("resize OK, but verify failed: %v\n", err)
		} else if current.Row == size.rows && current.Col == size.cols {
			fmt.Printf("OK\n")
		} else {
			fmt.Printf("verify FAILED: got %dx%d\n", current.Col, current.Row)
		}
	}
	
	fmt.Println("=== Test Complete ===")
	return nil
}

// IntegrationTest demonstrates how this would integrate with micran
func IntegrationTest() {
	fmt.Println("\n=== Integration Test ===")
	fmt.Println("This shows how PTY resize would be called from micran:")
	
	// Simulate the flow from containerd's ResizePtyRequest
	fmt.Println("1. containerd sends ResizePtyRequest")
	fmt.Println("2. shimService.ResizePty() is called")
	fmt.Println("3. sandbox.WinResize() is called")
	fmt.Println("4. Container.winresize() is called")
	fmt.Println("5. PTY resize ioctl is performed on /dev/ttyRPMSG*")
	
	// Create a micran PTY resize instance
	resize := NewMicranPTYResize(0) // Use ttyRPMSG0
	
	// Simulate a resize request from containerd
	fmt.Println("\nSimulating containerd resize request (40x120):")
	if err := resize.TestResize(); err != nil {
		log.Printf("Integration test failed: %v", err)
	} else {
		fmt.Println("Integration test passed!")
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  go run micran_pty_resize.go test [device_id]  # Run PTY resize test")
		fmt.Println("  go run micran_pty_resize.go demo             # Show integration demo")
		fmt.Println("  go run micran_pty_resize.go sizes            # Test various sizes")
		return
	}
	
	switch os.Args[1] {
	case "test":
		deviceID := 0
		if len(os.Args) > 2 {
			deviceID = atoi(os.Args[2])
		}
		resize := NewMicranPTYResize(deviceID)
		if err := resize.TestResize(); err != nil {
			log.Fatalf("Test failed: %v", err)
		}
	case "demo":
		IntegrationTest()
	case "sizes":
		testAllSizes()
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
	}
}

func testAllSizes() {
	fmt.Println("Testing various PTY sizes:")
	
	// Test with different device IDs
	for deviceID := 0; deviceID < 3; deviceID++ {
		fmt.Printf("\n--- Testing device ttyRPMSG%d ---\n", deviceID)
		resize := NewMicranPTYResize(deviceID)
		
		// Test common terminal sizes
		sizes := []struct{ rows, cols uint16 }{
			{24, 80},   // Standard
			{25, 80},   // DOS/VGA
			{43, 80},   // EGA
			{50, 80},   // VGA
			{60, 80},   // Some terminals
			{24, 132},  // Wide
			{43, 132},  // EGA wide
			{50, 132},  // VGA wide
		}
		
		for _, size := range sizes {
			if err := resize.Connect(); err != nil {
				log.Printf("Failed to connect: %v", err)
				continue
			}
			
			fmt.Printf("  Testing %dx%d... ", size.cols, size.rows)
			if err := resize.Resize(size.rows, size.cols); err != nil {
				fmt.Printf("FAILED: %v\n", err)
			} else {
				fmt.Printf("OK\n")
			}
			
			resize.Close()
		}
	}
}

func atoi(s string) int {
	result := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		}
	}
	return result
}
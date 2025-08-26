package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// Window size structure
type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// DummyRPMSGPTY simulates a /dev/ttyRPMSG device
type DummyRPMSGPTY struct {
	devicePath string
	masterFile *os.File
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.Mutex
	resizeChan chan *Winsize
}

// NewDummyRPMSGPTY creates a dummy RPMSG PTY device
func NewDummyRPMSGPTY(deviceID int) (*DummyRPMSGPTY, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	devicePath := fmt.Sprintf("/tmp/ttyRPMSG%d", deviceID)
	
	// Remove existing device if any
	os.Remove(devicePath)
	
	// Create a FIFO to simulate the RPMSG device
	if err := syscall.Mkfifo(devicePath, 0666); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create FIFO: %v", err)
	}
	
	pty := &DummyRPMSGPTY{
		devicePath: devicePath,
		ctx:        ctx,
		cancel:     cancel,
		resizeChan: make(chan *Winsize, 10),
	}
	
	return pty, nil
}

// Start starts the dummy PTY device
func (d *DummyRPMSGPTY) Start() error {
	// Start the device handler
	d.wg.Add(1)
	go d.deviceHandler()
	
	return nil
}

// deviceHandler handles the PTY device operations
func (d *DummyRPMSGPTY) deviceHandler() {
	defer d.wg.Done()
	
	// Open the FIFO for reading and writing
	// This will block until someone opens the other end
	fifo, err := os.OpenFile(d.devicePath, os.O_RDWR, 0666)
	if err != nil {
		log.Printf("Failed to open FIFO %s: %v", d.devicePath, err)
		return
	}
	defer fifo.Close()
	
	d.mu.Lock()
	d.masterFile = fifo
	d.mu.Unlock()
	
	log.Printf("Dummy RPMSG PTY %s ready", d.devicePath)
	
	// Send initial welcome message
	fifo.Write([]byte("Dummy RPMSG PTY connected. Window size: 80x24\n"))
	
	// Handle IO
	d.wg.Add(2)
	go d.handleInput(fifo)
	go d.handleOutput(fifo)
	
	// Wait for context cancellation
	<-d.ctx.Done()
	
	// Wait for handlers to finish
	d.wg.Wait()
	log.Printf("Dummy RPMSG PTY %s stopped", d.devicePath)
}

// handleInput handles input from the PTY
func (d *DummyRPMSGPTY) handleInput(fifo *os.File) {
	defer d.wg.Done()
	
	scanner := bufio.NewScanner(fifo)
	for scanner.Scan() {
		select {
		case <-d.ctx.Done():
			return
		default:
			input := scanner.Text()
			log.Printf("PTY Input: %s", input)
			
			// Handle special commands
			if input == "exit" {
				fifo.Write([]byte("Goodbye!\n"))
				d.cancel()
				return
			} else if input == "help" {
				fifo.Write([]byte("Available commands:\n"))
				fifo.Write([]byte("  help     - Show this help\n"))
				fifo.Write([]byte("  echo     - Echo text\n"))
				fifo.Write([]byte("  winsize  - Show current window size\n"))
				fifo.Write([]byte("  resize   - Simulate window resize\n"))
				fifo.Write([]byte("  exit     - Exit PTY\n"))
			} else if strings.HasPrefix(input, "echo ") {
				fifo.Write([]byte(strings.TrimPrefix(input, "echo ") + "\n"))
			} else if input == "winsize" {
				ws := d.getCurrentWinsize()
				fifo.Write([]byte(fmt.Sprintf("Current window size: %dx%d\n", ws.Col, ws.Row)))
			} else if input == "resize" {
				// Simulate a resize event
				newSize := &Winsize{
					Row: 40,
					Col: 120,
				}
				d.resizeChan <- newSize
				fifo.Write([]byte("Resize event sent (40x120)\n"))
			} else {
				fifo.Write([]byte(fmt.Sprintf("Unknown command: %s\n", input)))
			}
		}
	}
}

// handleOutput handles resize events and other output
func (d *DummyRPMSGPTY) handleOutput(fifo *os.File) {
	defer d.wg.Done()
	
	for {
		select {
		case <-d.ctx.Done():
			return
		case ws := <-d.resizeChan:
			// Simulate window resize notification
			fifo.Write([]byte(fmt.Sprintf("\nWindow resized to: %dx%d\n", ws.Col, ws.Row)))
		}
	}
}

// getCurrentWinsize gets the current window size
func (d *DummyRPMSGPTY) getCurrentWinsize() *Winsize {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	if d.masterFile == nil {
		return &Winsize{Row: 24, Col: 80}
	}
	
	var ws Winsize
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		d.masterFile.Fd(),
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return &Winsize{Row: 24, Col: 80}
	}
	
	return &ws
}

// ResizePTY resizes the PTY using ioctl
func (d *DummyRPMSGPTY) ResizePTY(rows, cols uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	if d.masterFile == nil {
		return fmt.Errorf("PTY not connected")
	}
	
	ws := Winsize{
		Row:    rows,
		Col:    cols,
		Xpixel: 0,
		Ypixel: 0,
	}
	
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		d.masterFile.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return fmt.Errorf("TIOCSWINSZ ioctl failed: %v", errno)
	}
	
	log.Printf("Resized PTY %s to %dx%d", d.devicePath, cols, rows)
	return nil
}

// Close closes the dummy PTY
func (d *DummyRPMSGPTY) Close() {
	d.cancel()
	d.wg.Wait()
	os.Remove(d.devicePath)
}

// InteractivePTYShell provides an interactive shell for testing PTY resize
type InteractivePTYShell struct {
	pty         *DummyRPMSGPTY
	connected   bool
	inputReader *os.File
	outputFile  *os.File
}

// NewInteractivePTYShell creates a new interactive PTY shell
func NewInteractivePTYShell(deviceID int) *InteractivePTYShell {
	return &InteractivePTYShell{
		inputReader: os.Stdin,
		outputFile:  os.Stdout,
	}
}

// Connect connects to the PTY device
func (s *InteractivePTYShell) Connect(deviceID int) error {
	var err error
	s.pty, err = NewDummyRPMSGPTY(deviceID)
	if err != nil {
		return fmt.Errorf("failed to create dummy PTY: %v", err)
	}
	
	if err := s.pty.Start(); err != nil {
		s.pty.Close()
		return fmt.Errorf("failed to start PTY: %v", err)
	}
	
	s.connected = true
	return nil
}

// Run runs the interactive shell
func (s *InteractivePTYShell) Run() error {
	if !s.connected {
		return fmt.Errorf("PTY not connected")
	}
	
	fmt.Printf("Connected to %s\n", s.pty.devicePath)
	fmt.Printf("Commands:\n")
	fmt.Printf("  /help      - Show help\n")
	fmt.Printf("  /resize H W - Resize PTY to H rows and W columns\n")
	fmt.Printf("  /winsize   - Show current window size\n")
	fmt.Printf("  /connect N - Connect to ttyRPMSGN\n")
	fmt.Printf("  /exit      - Exit\n")
	fmt.Printf("Any other input will be sent to the PTY\n\n")
	
	// Open the PTY for writing
	ptyFile, err := os.OpenFile(s.pty.devicePath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open PTY for writing: %v", err)
	}
	defer ptyFile.Close()
	
	// Start a goroutine to read PTY output
	outputDone := make(chan struct{})
	go func() {
		// Open another file descriptor for reading
		readFile, err := os.OpenFile(s.pty.devicePath, os.O_RDONLY, 0)
		if err != nil {
			log.Printf("Failed to open PTY for reading: %v", err)
			close(outputDone)
			return
		}
		defer readFile.Close()
		
		scanner := bufio.NewScanner(readFile)
		for scanner.Scan() {
			fmt.Printf("\rPTY> %s\n> ", scanner.Text())
		}
		close(outputDone)
	}()
	
	// Handle user input
	scanner := bufio.NewScanner(s.inputReader)
	fmt.Print("> ")
	
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		
		if strings.HasPrefix(input, "/") {
			// Handle shell commands
			switch input {
			case "/help":
				fmt.Printf("Commands:\n")
				fmt.Printf("  /help      - Show this help\n")
				fmt.Printf("  /resize H W - Resize PTY to H rows and W columns\n")
				fmt.Printf("  /winsize   - Show current window size\n")
				fmt.Printf("  /connect N - Connect to ttyRPMSGN\n")
				fmt.Printf("  /exit      - Exit\n")
			case "/winsize":
				ws := s.pty.getCurrentWinsize()
				fmt.Printf("Current PTY size: %dx%d\n", ws.Col, ws.Row)
			case "/exit":
				fmt.Printf("Exiting...\n")
				return nil
			default:
				if strings.HasPrefix(input, "/resize") {
					parts := strings.Fields(input)
					if len(parts) == 3 {
						rows, err1 := strconv.Atoi(parts[1])
						cols, err2 := strconv.Atoi(parts[2])
						if err1 == nil && err2 == nil {
							if err := s.pty.ResizePTY(uint16(rows), uint16(cols)); err != nil {
								fmt.Printf("Resize failed: %v\n", err)
							} else {
								fmt.Printf("Resized PTY to %dx%d\n", cols, rows)
								
								// Send resize command to trigger response
								ptyFile.Write([]byte("resize\n"))
							}
						} else {
							fmt.Printf("Invalid dimensions. Usage: /resize H W\n")
						}
					} else {
						fmt.Printf("Usage: /resize H W\n")
					}
				} else if strings.HasPrefix(input, "/connect") {
					parts := strings.Fields(input)
					if len(parts) == 2 {
						deviceID, err := strconv.Atoi(parts[1])
						if err == nil {
							s.pty.Close()
							if err := s.Connect(deviceID); err != nil {
								fmt.Printf("Failed to connect to ttyRPMSG%d: %v\n", deviceID, err)
							} else {
								fmt.Printf("Connected to ttyRPMSG%d\n", deviceID)
								return s.Run() // Restart with new connection
							}
						} else {
							fmt.Printf("Invalid device ID\n")
						}
					} else {
						fmt.Printf("Usage: /connect N\n")
					}
				} else {
					fmt.Printf("Unknown command: %s\n", input)
				}
			}
		} else {
			// Send input to PTY
			if _, err := ptyFile.Write([]byte(input + "\n")); err != nil {
				fmt.Printf("Failed to write to PTY: %v\n", err)
			}
		}
		
		fmt.Print("> ")
	}
	
	return scanner.Err()
}

// Close closes the interactive shell
func (s *InteractivePTYShell) Close() {
	if s.pty != nil {
		s.pty.Close()
	}
	s.connected = false
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  go run interactive_pty.go device [device_id]")
		fmt.Println("  go run interactive_pty.go server [device_id]")
		return
	}
	
	switch os.Args[1] {
	case "device":
		deviceID := 0
		if len(os.Args) > 2 {
			if id, err := strconv.Atoi(os.Args[2]); err == nil {
				deviceID = id
			}
		}
		runDeviceMode(deviceID)
	case "server":
		deviceID := 0
		if len(os.Args) > 2 {
			if id, err := strconv.Atoi(os.Args[2]); err == nil {
				deviceID = id
			}
		}
		runServerMode(deviceID)
	default:
		fmt.Printf("Unknown mode: %s\n", os.Args[1])
	}
}

func runDeviceMode(deviceID int) {
	fmt.Printf("Starting dummy RPMSG PTY device ttyRPMSG%d\n", deviceID)
	fmt.Printf("Device will be available at /tmp/ttyRPMSG%d\n", deviceID)
	fmt.Printf("Test with: cat /tmp/ttyRPMSG%d (in another terminal)\n", deviceID)
	fmt.Printf("Press Ctrl+C to exit\n")
	
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	
	pty, err := NewDummyRPMSGPTY(deviceID)
	if err != nil {
		log.Fatalf("Failed to create dummy PTY: %v", err)
	}
	defer pty.Close()
	
	if err := pty.Start(); err != nil {
		log.Fatalf("Failed to start PTY: %v", err)
	}
	
	// Wait for interrupt signal
	<-sigChan
	fmt.Printf("\nShutting down...\n")
}

func runServerMode(deviceID int) {
	fmt.Printf("Interactive PTY Shell for ttyRPMSG%d\n", deviceID)
	
	shell := NewInteractivePTYShell(deviceID)
	defer shell.Close()
	
	if err := shell.Connect(deviceID); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	
	if err := shell.Run(); err != nil {
		log.Printf("Shell error: %v", err)
	}
	
	fmt.Printf("Shell exited\n")
}
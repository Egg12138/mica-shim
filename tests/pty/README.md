# PTY Resize Test Suite for Micran

This directory contains comprehensive tests for PTY (pseudo-terminal) resize functionality in Micran, simulating the behavior of `/dev/ttyRPMSG*` devices used by MICA RTOS.

## Overview

The test suite includes:

1. **PTY Experiment** (`pty_experiment/`)
   - Tests virtual PTY and FIFO communication
   - Demonstrates bidirectional communication patterns
   - Includes dummy shell simulation

2. **PTY Test** (`pty_test/`)
   - Tests PTY functionality with FIFO and stdio
   - Simple dummy shell implementation

3. **PTY Resize Demo** (`pty_resize_demo/`)
   - Tests basic ioctl-based PTY resize operations
   - Verifies window size changes
   - Includes shell integration tests

4. **Micran PTY Resize** (`micran_pty_resize/`)
   - Implements the actual PTY resize logic for Micran
   - Tests integration with the micran architecture
   - Demonstrates ioctl usage on `/dev/ttyRPMSG*` devices

5. **Dummy Shell** (`dummy_shell/`)
   - Simple shell implementation for testing
   - Demonstrates basic command processing

6. **Interactive PTY** (`interactive_pty/`)
   - Simulates `/dev/ttyRPMSG*` devices for testing
   - Provides interactive shell for manual testing
   - Supports device mode and server mode

## Key Concepts

### PTY Resize in Micran

Micran uses PTY resize to propagate terminal window size changes from containerd to the RTOS container. The flow is:

1. `containerd` sends `ResizePtyRequest` to micran shim
2. `shimService.ResizePty()` handles the request
3. `sandbox.WinResize()` forwards to container
4. `Container.winresize()` performs ioctl on `/dev/ttyRPMSG*`
5. RTOS receives the resize notification via RPMsg

### IOCTL Implementation

The resize operation uses the `TIOCSWINSZ` ioctl command:

```go
ws := Winsize{Row: rows, Col: cols, Xpixel: 0, Ypixel: 0}
syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
```

This is the same approach used by kata-containers and other container runtimes.

## Test Files

### pty_resize_test.go

Comprehensive PTY resize testing including:

- **Basic resize test**: Tests various window sizes
- **Shell integration test**: Tests resize with running shell
- **Interactive demo**: Shows periodic resize operations

Usage:
```bash
# Basic resize test
make pty-resize-basic
# or
cd pty_resize_demo && go run pty_resize_demo.go basic

# Resize with shell test
make pty-resize-shell
# or
cd pty_resize_demo && go run pty_resize_demo.go shell

# Interactive demo
make pty-resize-demo
# or
cd pty_resize_demo && go run pty_resize_demo.go demo
```

### micran_pty_resize.go

Micran-specific PTY resize implementation:

- **Integration test**: Shows full integration flow
- **Device testing**: Tests with different device IDs
- **Size verification**: Verifies resize operations

Usage:
```bash
# Run micran resize test
go run micran_pty_resize.go test

# Show integration demo
go run micran_pty_resize.go demo

# Test various sizes
go run micran_pty_resize.go sizes
```

### interactive_pty.go

Interactive PTY simulator for manual testing:

- **Device mode**: Creates a dummy `/dev/ttyRPMSG*` device
- **Server mode**: Provides interactive shell
- **Resize commands**: Supports manual resize testing

Usage:
```bash
# Start dummy device (ttyRPMSG0)
go run interactive_pty.go device

# Start interactive shell
go run interactive_pty.go server

# Connect to specific device
go run interactive_pty.go server 1
```

## Makefile Targets

The Makefile provides convenient targets for running tests:

```bash
# Basic PTY tests
make pty-test
make fifo-test
make stdio-test

# PTY resize tests
make pty-resize-basic      # Basic resize functionality
make pty-resize-shell      # Resize with shell
make pty-resize-demo       # Interactive demo

# Micran-specific tests
make micran-resize-test    # Micran resize test
make micran-resize-demo    # Integration demo
make micran-resize-sizes   # Test various sizes

# Interactive tests
make interactive-pty-device   # Start dummy device
make interactive-pty-server   # Start interactive shell

# Run all tests
make test-all-resize

# Clean up
make clean
```

## Manual Testing

### 1. Basic PTY Resize Test

```bash
# Using make
make pty-resize-basic

# Or directly
cd tests/pty/pty_resize_demo
go run pty_resize_demo.go basic
```

### 2. Interactive Testing

```bash
# Terminal 1: Start dummy device
make interactive-pty-device
# or
cd tests/pty/interactive_pty && go run interactive_pty.go device 0

# Terminal 2: Connect to device
make interactive-pty-server
# or
cd tests/pty/interactive_pty && go run interactive_pty.go server 0

# In the interactive shell:
> /help              # Show help
> /resize 40 120     # Resize to 40x120
> /winsize           # Show current size
> echo hello         # Send to PTY
> /exit              # Exit
```

### 3. Micran Integration Test

```bash
# Using make
make micran-resize-test

# Or directly
cd tests/pty/micran_pty_resize
go run micran_pty_resize.go test
```

## Expected Output

### Successful Resize Test

```
=== PTY Resize Test Started ===
Original window size: 80x24
Testing resize to Standard 24x80... OK
Testing resize to Large 40x120... OK
Testing resize to Small 10x40... OK
Testing resize to Wide 25x132... OK
Testing resize to Extra Large 50x200... OK
Restoring original size... OK
=== PTY Resize Test Completed ===
```

### Interactive Session

```
Connected to /tmp/ttyRPMSG0
Commands:
  /help      - Show help
  /resize H W - Resize PTY to H rows and W columns
  /winsize   - Show current window size
  /connect N - Connect to ttyRPMSGN
  /exit      - Exit

> /resize 40 120
Resized PTY to 40x120
PTY> Window resized to: 40x120
> 
```

## Implementation Details

### Winsize Structure

```go
type Winsize struct {
    Row    uint16  // Number of rows
    Col    uint16  // Number of columns
    Xpixel uint16  // X pixel size (usually 0)
    Ypixel uint16  // Y pixel size (usually 0)
}
```

### Error Handling

The tests handle various error conditions:

- PTY device not available
- Ioctl operation failures
- Invalid window sizes
- Permission errors

### Platform Compatibility

- **Linux**: Full support with ioctl
- **Other platforms**: Limited or no support (ioctl is Linux-specific)

## Integration with Micran

To integrate these tests with the main micran codebase:

1. Copy the resize logic from `micran_pty_resize.go` to `pkg/micantainer/container.go`
2. Update the `winresize` method to use ioctl
3. Ensure proper error handling and logging
4. Add unit tests to the main test suite

## Troubleshooting

### Common Issues

1. **Permission denied**: Ensure user has access to `/dev/ptmx`
2. **Device not found**: Install `bsdutils` or `util-linux` package
3. **Ioctl fails**: Check kernel support for PTY operations

### Debug Commands

```bash
# Check PTY devices
ls -la /dev/pt*

# Check kernel support
lsmod | grep pty

# Test PTY creation
python -c "import pty; master, slave = pty.openpty()"
```

## References

- [Linux TTY Driver](https://www.kernel.org/doc/Documentation/tty/tty.txt)
- [Ioctl operations](https://man7.org/linux/man-pages/man2/ioctl_tty.2.html)
- [kata-containers implementation](https://github.com/kata-containers/kata-containers)
- [containerd shim v2 protocol](https://github.com/containerd/containerd)
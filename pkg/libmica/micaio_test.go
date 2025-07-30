package libmica

import (
	"context"
	"os"
	"syscall"
	"testing"
)

// TestStdinFIFOReader tests the basic functionality of stdin FIFO reader
func TestStdinFIFOReader(t *testing.T) {
	// Create a temporary FIFO for testing
	tempFIFO := "/tmp/test_stdin_fifo"
	if err := syscall.Mkfifo(tempFIFO, 0666); err != nil {
		t.Fatalf("Failed to create test FIFO: %v", err)
	}
	defer os.Remove(tempFIFO)

	// Test creating a new stdin FIFO reader
	reader, err := newStdinFIFOReader(tempFIFO, "test-task")
	if err != nil {
		t.Fatalf("Failed to create stdin FIFO reader: %v", err)
	}
	defer reader.Close()

	// Verify the reader was created properly
	if reader.taskID != "test-task" {
		t.Errorf("Expected taskID 'test-task', got %s", reader.taskID)
	}

	if reader.file == nil {
		t.Error("Expected file to be non-nil")
	}
}

// TestStdinFIFOReaderEmptyPath tests error handling for empty path
func TestStdinFIFOReaderEmptyPath(t *testing.T) {
	_, err := newStdinFIFOReader("", "test-task")
	if err == nil {
		t.Error("Expected error for empty stdin path, got nil")
	}

	expectedMsg := "stdin path is empty"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

// TestMicaIOWithStdin tests MicaIO creation with stdin FIFO path
func TestMicaIOWithStdin(t *testing.T) {
	ctx := context.Background()
	tempFIFO := "/tmp/test_stdin_fifo_micaio"

	// Create temporary FIFOs for testing
	if err := syscall.Mkfifo(tempFIFO, 0666); err != nil {
		t.Fatalf("Failed to create test FIFO: %v", err)
	}
	defer os.Remove(tempFIFO)

	// Create MicaIO with stdin
	micaIO, err := NewMicaIO(ctx, "test-task", tempFIFO, "", "", false)
	if err != nil {
		t.Fatalf("Failed to create MicaIO: %v", err)
	}
	defer micaIO.Close()

	// Verify stdin FIFO path is stored
	if micaIO.stdinFIFOPath != tempFIFO {
		t.Errorf("Expected stdinFIFOPath '%s', got '%s'", tempFIFO, micaIO.stdinFIFOPath)
	}

	// Verify stdin PipeIO is also created
	if micaIO.stdin == nil {
		t.Error("Expected stdin PipeIO to be created")
	}
}

// TestMicaIOWithoutStdin tests MicaIO creation without stdin
func TestMicaIOWithoutStdin(t *testing.T) {
	ctx := context.Background()

	// Create MicaIO without stdin
	micaIO, err := NewMicaIO(ctx, "test-task", "", "", "", false)
	if err != nil {
		t.Fatalf("Failed to create MicaIO: %v", err)
	}
	defer micaIO.Close()

	// Verify no stdin FIFO path is stored
	if micaIO.stdinFIFOPath != "" {
		t.Errorf("Expected empty stdinFIFOPath, got '%s'", micaIO.stdinFIFOPath)
	}

	// Verify no stdin PipeIO is created
	if micaIO.stdin != nil {
		t.Error("Expected stdin PipeIO to be nil")
	}
}

// TestStdinForwardingWithoutPTY tests stdin forwarding when PTY is not available
func TestStdinForwardingWithoutPTY(t *testing.T) {
	ctx := context.Background()
	tempFIFO := "/tmp/test_stdin_fifo_no_pty"

	if err := syscall.Mkfifo(tempFIFO, 0666); err != nil {
		t.Fatalf("Failed to create test FIFO: %v", err)
	}
	defer os.Remove(tempFIFO)

	micaIO, err := NewMicaIO(ctx, "test-task", tempFIFO, "", "", false)
	if err != nil {
		t.Fatalf("Failed to create MicaIO: %v", err)
	}
	defer micaIO.Close()

	// Try to forward stdin without PTY (should return immediately)
	err = micaIO.forwardStdinToPTY()
	if err != nil {
		t.Errorf("Expected no error when PTY is not available, got: %v", err)
	}
}

// TestStdinForwardingErrorHandling tests error handling in stdin forwarding
func TestStdinForwardingErrorHandling(t *testing.T) {
	ctx := context.Background()

	micaIO := &MicaIO{
		taskID:        "test-task",
		ctx:           ctx,
		stdinFIFOPath: "/nonexistent/path",
	}

	// Mock a PTY file (we'll use a regular file for testing)
	tempFile, err := os.CreateTemp("", "mock_pty")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	micaIO.ptyFile = tempFile

	// Try to forward stdin with nonexistent FIFO path
	err = micaIO.forwardStdinToPTY()
	if err == nil {
		t.Error("Expected error for nonexistent FIFO path, got nil")
	}
}

// BenchmarkStdinForwarding benchmarks the stdin forwarding performance
func BenchmarkStdinForwarding(b *testing.B) {
	// This would require a more complex setup with actual PTY devices
	// For now, we'll skip this benchmark
	b.Skip("Benchmark requires full PTY setup")
}

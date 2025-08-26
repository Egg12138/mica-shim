.PHONY: all clean pty-test fifo-test stdio-test dummy-shell \
	pty-resize-basic pty-resize-shell pty-resize-demo \
	micran-resize-test micran-resize-demo micran-resize-sizes \
	interactive-pty-device interactive-pty-server

all: pty-test fifo-test stdio-test dummy-shell pty-resize-basic micran-resize-test

# Original tests
pty-test:
	cd pty_experiment && go run pty_experiment.go pty

fifo-test:
	cd pty_experiment && go run pty_experiment.go fifo

stdio-test:
	cd pty_test && go run main.go test

dummy-shell:
	cd dummy_shell && go run main.go

# PTY resize tests
pty-resize-basic:
	cd pty_resize_demo && go run pty_resize_demo.go basic

pty-resize-shell:
	cd pty_resize_demo && go run pty_resize_demo.go shell

pty-resize-demo:
	cd pty_resize_demo && go run pty_resize_demo.go demo

# Micran-specific PTY resize tests
micran-resize-test:
	cd micran_pty_resize && go run micran_pty_resize.go test

micran-resize-demo:
	cd micran_pty_resize && go run micran_pty_resize.go demo

micran-resize-sizes:
	cd micran_pty_resize && go run micran_pty_resize.go sizes

# Interactive PTY tests
interactive-pty-device:
	cd interactive_pty && go run interactive_pty.go device

interactive-pty-server:
	cd interactive_pty && go run interactive_pty.go server

# Test all resize functionality
test-all-resize: pty-resize-basic pty-resize-shell micran-resize-test micran-resize-demo
	@echo "All resize tests completed"

# Clean up temporary files
clean:
	rm -f /tmp/ttyDummy
	rm -f /tmp/ttyRPMSG*
	@echo "Cleaned up temporary PTY devices"

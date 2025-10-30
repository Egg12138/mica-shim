# Makefile for Enhanced Mock Micad
#
# This builds the mock_micad with RTOS IO simulation support

CC = gcc
CFLAGS = -Wall -Wextra -std=gnu99 -O2 -g
LDFLAGS = -pthread -lrt

TARGET = mock_micad
SRC = mock_micad.c

SHELL_TARGET = shell
SHELL_SRC = shell.c
SHELL_LDFLAGS =

CLIENT_SHELL_TARGET = client_shell
CLIENT_SHELL_SRC = client_shell.c
CLIENT_SHELL_LDFLAGS =

.PHONY: all clean clean-all gc run test help ctl build-shell build-client-shell build-all

all: $(TARGET)

build-all: $(TARGET) $(SHELL_TARGET) $(CLIENT_SHELL_TARGET)
	@echo "Built all components: mock_micad, shell, and client_shell"

$(TARGET): $(SRC)
	$(CC) $(CFLAGS) -o $@ $< $(LDFLAGS)

$(SHELL_TARGET): $(SHELL_SRC)
	$(CC) $(CFLAGS) -o $@ $< $(SHELL_LDFLAGS)

$(CLIENT_SHELL_TARGET): $(CLIENT_SHELL_SRC)
	$(CC) $(CFLAGS) -o $@ $< $(CLIENT_SHELL_LDFLAGS)

clean:
	rm -f $(TARGET) $(SHELL_TARGET) $(CLIENT_SHELL_TARGET)

clean-all: clean gc ## Clean build artifacts and leftover resources

gc: ## Garbage collection - cleanup leftover resources
	@echo "🧹 Cleaning up leftover mock_micad resources..."
	@echo "Cleaning up client sockets..."
	@find /tmp/mica -name "*.socket" -type s -delete 2>/dev/null || true
	@echo "Cleaning up PTY symlinks..."
	@find /tmp/mica -name "ttyRPMSG_*" -type l -delete 2>/dev/null || true
	@find /dev -name "ttyRPMSG_*" -type l -delete 2>/dev/null || true
	@echo "Cleaning up main socket..."
	@rm -f /tmp/mica/mica-create.socket
	@echo "Cleaning up socket directory..."
	@rmdir /tmp/mica 2>/dev/null || true
	@echo "✅ Garbage collection completed"

test: $(TARGET)
	@echo "Testing mock_micad..."
	./$(TARGET) -q &
	@sleep 2
	@echo "Mock micad is running. Press Ctrl+C to stop."
	@read -p "Press Enter to stop test..."
	@pkill -f mock_micad || true

run: $(TARGET)
	@echo "Running mock_micad..."
	./$(TARGET)

ctl:
	@echo "Mock micad control helper"
	@echo "  go run ./mock_ctl.go --id <container-id> status"
	@echo "  go run ./mock_ctl.go --id <container-id> start"
	@echo "  go run ./mock_ctl.go --id <container-id> tail"
	@echo "Flags:"
	@echo "  --socket-dir=/tmp/mica        Control socket directory"
	@echo "  --create-socket=/tmp/mica/mica-create.socket  Fallback to mica-create"
	@echo "  --create                       Force use of mica-create socket"

build-shell: $(SHELL_TARGET)
	@echo "Interactive shell for mock_micad"
	@echo "  ./shell                        # Run interactive shell"
	@echo "Commands available in shell:"
	@echo "  create <name>                  # Create a new client"
	@echo "  start <name>                   # Start a client"
	@echo "  stop <name>                    # Stop a client"
	@echo "  status <name>                  # Get client status"
	@echo "  rm <name>                      # Remove a client"
	@echo "  tail <name>                    # Tail PTY output"
	@echo "  help                           # Show available commands"
	@echo "  quit/exit                      # Exit shell"

build-client-shell: $(CLIENT_SHELL_TARGET)
	@echo "Client shell for mock_micad"
	@echo "  ./client_shell                 # Run client shell (usually spawned by mock_micad)"
	@echo "Commands available in client shell:"
	@echo "  help                           # Show available commands"
	@echo "  status                         # Show shell status"
	@echo "  uptime                         # Show simulated uptime"
	@echo "  info                           # Show shell information"
	@echo "  quit/exit                      # Exit shell"

help:
	@echo "Mock Micad Build System"
	@echo ""
	@echo "Targets:"
	@echo "  all             - Build mock_micad"
	@echo "  build-all       - Build all components (mock_micad + shells)"
	@echo "  run             - Run mock_micad"
	@echo "  build-shell     - Build interactive shell"
	@echo "  build-client-shell - Build client shell (spawned by mock_micad)"
	@echo "  ctl             - Show helper usage for mock_ctl.go"
	@echo "  clean           - Remove build artifacts"
	@echo "  clean-all       - Clean build artifacts and leftover resources"
	@echo "  gc              - Garbage collection - cleanup leftover resources"
	@echo "  test            - Test mock_micad execution"
	@echo "  help            - Show this help message"
	@echo ""
	@echo "Usage:"
	@echo "  make build-all  # Build everything needed"
	@echo "  make run        # Run mock_micad (spawns client shells automatically)"
	@echo "  make build-shell # Build standalone interactive shell"
	@echo "  make test       # Test execution"
	@echo "  make clean-all  # Clean build artifacts and leftover resources"
	@echo "  make gc         # Cleanup leftover resources (sockets, PTYs)"
	@echo "  ./mock_micad    # Run with response mode"
	@echo "  ./mock_micad -q # Run in quiet mode (no responses)"
	@echo "  ./shell         # Run standalone interactive shell"
	@echo "  ./client_shell  # Run client shell directly (for testing)" 

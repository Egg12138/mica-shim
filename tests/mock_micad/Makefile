# Makefile for Enhanced Mock Micad
#
# This builds the mock_micad with RTOS IO simulation support

CC = gcc
CFLAGS = -Wall -Wextra -std=gnu99 -O2 -g
LDFLAGS = -pthread -lrt

TARGET = mock_micad
SRC = mock_micad.c

.PHONY: all clean run test help

all: $(TARGET)

$(TARGET): $(SRC)
	$(CC) $(CFLAGS) -o $@ $< $(LDFLAGS)

clean:
	rm -f $(TARGET)

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

help:
	@echo "Mock Micad Build System"
	@echo ""
	@echo "Targets:"
	@echo "  all    - Build mock_micad"  
	@echo "  run    - Run mock_micad"
	@echo "  clean  - Remove build artifacts"
	@echo "  test   - Test mock_micad execution"
	@echo "  help   - Show this help message"
	@echo ""
	@echo "Usage:"
	@echo "  make           # Build mock_micad"
	@echo "  make run       # Run mock_micad"
	@echo "  make test      # Test execution"
	@echo "  ./mock_micad   # Run with response mode"
	@echo "  ./mock_micad -q # Run in quiet mode (no responses)" 

#!/bin/bash
# Test script for sim_linux_tty - runs a bash shell in a PTY for testing

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Build the project first
echo -e "${GREEN}Building sim_linux_tty...${NC}"
make native

# Check if build succeeded
if [ ! -f "bin/sim_linux_tty-native" ]; then
    echo -e "${RED}Build failed! Please check the error messages above.${NC}"
    exit 1
fi

# Create output directory
mkdir -p out

# Start sim_linux_tty in background
echo -e "${YELLOW}Starting sim_linux_tty with bash shell...${NC}"
./bin/sim_linux_tty-native &
SIM_PID=$!

# Give it a moment to start
sleep 0.5

# Check if sim_linux_tty is still running
if ! kill -0 $SIM_PID 2>/dev/null; then
    echo -e "${RED}sim_linux_tty failed to start!${NC}"
    exit 1
fi

# Wait for PTY to be ready
sleep 1

# Start host-driver with the PTY device
echo -e "${YELLOW}Starting host-driver connected to sim_linux_tty...${NC}"
echo -e "${YELLOW}This will connect to a real bash shell!${NC}"
echo ""

# Set PTY device environment
export MICRAN_PTY_DEVICE=/tmp/ttyRPMSG3

# Run host-driver
./bin/host-driver-native -pty /tmp/ttyRPMSG3 -terminal true

# Clean up on exit
kill $SIM_PID 2>/dev/null || true
wait $SIM_PID 2>/dev/null || true

echo -e "${GREEN}Test completed.${NC}"
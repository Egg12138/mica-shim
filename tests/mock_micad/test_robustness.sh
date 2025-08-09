#!/bin/bash

# Test script for mock_micad socket robustness

echo "Testing mock_micad socket robustness..."

# Start mock_micad in background
./mock_micad -q &
MOCK_PID=$!

# Give it time to start
sleep 2

# Check if it's running
if kill -0 $MOCK_PID 2>/dev/null; then
    echo "mock_micad is running with PID $MOCK_PID"
else
    echo "mock_micad failed to start"
    exit 1
fi

# Check if socket was created
if [ -S "/tmp/mica/mica-create.socket" ]; then
    echo "Main socket created successfully"
else
    echo "Main socket not found"
    kill $MOCK_PID
    exit 1
fi

# Remove the socket file to simulate unexpected removal
echo "Removing main socket to test robustness..."
rm /tmp/mica/mica-create.socket

# Wait a few seconds for mock_micad to detect and recreate the socket
sleep 3

# Check if socket was recreated
if [ -S "/tmp/mica/mica-create.socket" ]; then
    echo "Main socket was successfully recreated!"
else
    echo "Main socket was NOT recreated"
fi

# Clean up
echo "Stopping mock_micad..."
kill $MOCK_PID
sleep 1

# Final check
if kill -0 $MOCK_PID 2>/dev/null; then
    echo "mock_micad did not stop properly"
    kill -9 $MOCK_PID
else
    echo "mock_micad stopped successfully"
fi

# Clean up any created files
rm -f /tmp/mica/mica-create.socket
rm -f /tmp/mica/*.socket
rm -f /tmp/ttyRPMSGtest_client /tmp/ttyRPMSGdemo 2>/dev/null

echo "Robustness test completed"
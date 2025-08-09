#!/bin/bash


echo "Starting mock_micad test..."

./mock_micad -q &
MOCK_PID=$!

sleep 2

if kill -0 $MOCK_PID 2>/dev/null; then
    echo "mock_micad is running with PID $MOCK_PID"
else
    echo "mock_micad failed to start"
    exit 1
fi

if [ -S "/tmp/mica/mica-create.socket" ]; then
    echo "Main socket created successfully"
else
    echo "Main socket not found"
    kill $MOCK_PID
    exit 1
fi

echo "Testing client creation..."

if command -v socat &> /dev/null; then
    echo "test_client" | socat - UNIX-CLIENT:/tmp/mica/mica-create.socket &
    sleep 1
    
    if [ -S "/tmp/mica/test_client.socket" ]; then
        echo "Client socket created successfully"
    else
        echo "Client socket not found"
    fi
    
    # Check if a mock PTY file was created
    if [ -f "/tmp/ttyRPMSGtest_client" ]; then
        echo "Mock PTY file created successfully"
        echo "Initial contents:"
        cat /tmp/ttyRPMSGtest_client
        
        sleep 3
        echo "Contents after waiting:"
        cat /tmp/ttyRPMSGtest_client
    else
        echo "Mock PTY file not found"
    fi
else
    echo "socat not available, skipping client creation test"
fi

# Clean up
echo "Stopping mock_micad..."
kill $MOCK_PID
sleep 1

if kill -0 $MOCK_PID 2>/dev/null; then
    echo "mock_micad did not stop properly"
    kill -9 $MOCK_PID
else
    echo "mock_micad stopped successfully"
fi

rm -f /tmp/mica/mica-create.socket
rm -f /tmp/mica/*.socket
rm -f /tmp/ttyRPMSGtest_client /tmp/ttyRPMSGdemo 2>/dev/null

echo "Test completed"
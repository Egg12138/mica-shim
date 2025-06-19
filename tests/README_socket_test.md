# Socket Communication Tests

This directory contains tests to verify that `socket.go` can communicate with `mock_micad` exactly like `mica.py` does.

## Files

- `test_socket_communication.go` - Standalone test program
- `mock_micad/` - Mock micad server for testing

## How to Run Tests

### 1. Start mock_micad

```bash
# Terminal 1: Start mock_micad
cd tests/mock_micad
make
./mock_micad
```

You should see:
```
Mock micad started. Listening on /tmp/mica/mica-create.socket
Press Ctrl+C to stop
Response mode: enabled
```

### 2. Run Go tests

```bash
# Terminal 2: Run socket.go tests
cd tests
go run test_socket_communication.go
```

### 3. Compare with mica.py

```bash
# Terminal 3: Run mica.py for comparison
cd scripts
python3 mica.py create qemu-zephyr-rproc.conf
```

## Expected Results

### mock_micad Output (Terminal 1)

When running **Go test**:
```
Received data (325 bytes):
03 00 00 00 71 65 6d 75 2d 7a 65 70 68 79 72 00 ...

Received input as string: \x03***qemu-zephyr***/home/egg/playground/zephr.elf***...

Received Create Message:
CPU: 3
Name: qemu-zephyr
Path: /home/egg/playground/zephr.elf
Ped: 
PedCfg: 
Debug: false
```

When running **mica.py**:
```
Received data (325 bytes):
03 00 00 00 71 65 6d 75 2d 7a 65 70 68 79 72 00 ...

Received input as string: \x03***qemu-zephyr***/home/egg/playground/zephr.elf***...

Received Create Message:
CPU: 3
Name: qemu-zephyr
Path: /home/egg/playground/zephr.elf
Ped: 
PedCfg: 
Debug: false
```

### Go Test Output (Terminal 2)

```
🧪 Testing socket.go communication with mock_micad
📋 Make sure mock_micad is running first!

✅ Found mock_micad socket: /tmp/mica/mica-create.socket

=== Test 1: MicaCreate (struct message) ===
📤 Sending create message:
   CPU: 3
   Name: qemu-zephyr
   Path: /home/egg/playground/zephr.elf
   Debug: false
   Total size: 325 bytes
📥 Response: MICA-SUCCESS
✅ MicaCreate test PASSED!

=== Test 2: Control Commands (string messages) ===
📤 Sending 'start' command to client: qemu-zephyr
⚠️  Start command failed (expected): mica socket directory does not exist, please check if micad is running
✅ Control commands test completed!

=== Test 3: Message Packing Verification ===
📤 Testing message packing via TestCreate...
📥 TestCreate response: MICA-SUCCESS
✅ Message packing test PASSED!

✅ All tests completed!
📝 Check mock_micad output to verify messages were received correctly
```

## What Each Test Does

1. **MicaCreate Test**: Sends a 325-byte binary struct (same as mica.py)
2. **Control Commands Test**: Attempts to send string commands to client sockets
3. **Message Packing Test**: Verifies the binary message format

## Success Criteria

✅ **socket.go works correctly if**:
- mock_micad receives exactly 325 bytes for create messages
- CPU field shows `3` (not `0`)
- Name field shows `qemu-zephyr`
- Path field shows `/home/egg/playground/zephr.elf`
- The hex dump matches between Go and Python versions
- Both return `MICA-SUCCESS`

## Troubleshooting

### "socket not connected" error
- Make sure mock_micad is running first
- Check that `/tmp/mica/mica-create.socket` exists

### Different output than mica.py
- Check that socket.go CPU field is set to `3`
- Verify message packing is identical
- Compare hex dumps byte-by-byte

### Control commands fail
- This is expected - client sockets don't exist in mock mode
- The important test is the create message (325-byte struct) 
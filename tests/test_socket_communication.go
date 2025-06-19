package main

import (
	"fmt"
	"os"
	"time"

	"mica-shim/libmica"
)

func main() {
	fmt.Println("🧪 Testing socket.go communication with mock_micad")
	fmt.Println("📋 Make sure mock_micad is running first!")
	fmt.Println()

	// Check if mock_micad is running
	checkMockMicad()

	// Test 1: Create message (325-byte struct) - like mica.py
	fmt.Println("=== Test 1: MicaCreate (struct message) ===")
	testMicaCreate()

	// Wait a bit between tests
	time.Sleep(1 * time.Second)

	// Test 2: Control commands (string messages) - like socat
	fmt.Println("=== Test 2: Control Commands (string messages) ===")
	testControlCommands()

	// Test 3: Message packing verification
	fmt.Println("=== Test 3: Message Packing Verification ===")
	testMessagePacking()

	fmt.Println("✅ All tests completed!")
	fmt.Println("📝 Check mock_micad output to verify messages were received correctly")
}

func testMicaCreate() {
	// Create message with exact same parameters as mica.py
	config := libmica.NewMicaCreateMsg(
		3,                                // CPU=3 (from qemu-zephyr-rproc.conf)
		"qemu-zephyr",                    // Name (from config file)
		"/home/egg/playground/zephr.elf", // Path (from config file)
		"",                               // Ped (empty)
		"",                               // PedCfg (empty)
		false,                            // Debug=false
	)

	fmt.Printf("📤 Sending create message:\n")
	fmt.Printf("   CPU: %d\n", 3)
	fmt.Printf("   Name: qemu-zephyr\n")
	fmt.Printf("   Path: /home/egg/playground/zephr.elf\n")
	fmt.Printf("   Debug: false\n")
	fmt.Printf("   Total size: 325 bytes\n")

	response, err := libmica.MicaCreate(config)
	if err != nil {
		fmt.Printf("❌ MicaCreate failed: %v\n", err)
		fmt.Println("   Make sure mock_micad is running!")
		return
	}

	fmt.Printf("📥 Response: %s\n", response)

	if response == "MICA-SUCCESS" {
		fmt.Println("✅ MicaCreate test PASSED!")
		fmt.Println("   mock_micad should show:")
		fmt.Println("   - 325 bytes received")
		fmt.Println("   - CPU: 3")
		fmt.Println("   - Name: qemu-zephyr")
		fmt.Println("   - Path: /home/egg/playground/zephr.elf")
	} else {
		fmt.Printf("❌ Expected MICA-SUCCESS, got: %s\n", response)
	}
	fmt.Println()
}

func testControlCommands() {
	client := "qemu-zephyr"

	// Test start command (this will likely fail since client socket doesn't exist)
	fmt.Printf("📤 Sending 'start' command to client: %s\n", client)
	response, err := libmica.MicaCtl(libmica.MStart, client)
	if err != nil {
		fmt.Printf("⚠️  Start command failed (expected): %v\n", err)
		fmt.Println("   This is normal - client socket doesn't exist yet")
	} else {
		fmt.Printf("📥 Start response: %s\n", response)
	}

	// Test status command
	fmt.Printf("📤 Sending 'status' command to client: %s\n", client)
	response, err = libmica.MicaCtl(libmica.MStatus, client)
	if err != nil {
		fmt.Printf("⚠️  Status command failed (expected): %v\n", err)
		fmt.Println("   This is normal - client socket doesn't exist yet")
	} else {
		fmt.Printf("📥 Status response: %s\n", response)
	}

	fmt.Println("✅ Control commands test completed!")
	fmt.Println()
}

func testMessagePacking() {
	fmt.Println("📤 Testing message packing via TestCreate...")

	response, err := libmica.TestCreate()
	if err != nil {
		fmt.Printf("❌ TestCreate failed: %v\n", err)
		return
	}

	fmt.Printf("📥 TestCreate response: %s\n", response)

	if response == "MICA-SUCCESS" {
		fmt.Println("✅ Message packing test PASSED!")
		fmt.Println("   The 325-byte struct was correctly packed and sent")
		fmt.Println("   mock_micad should show the same format as mica.py")
	} else {
		fmt.Printf("❌ Expected MICA-SUCCESS, got: %s\n", response)
	}
	fmt.Println()
}

// Helper function to check if mock_micad is running
func checkMockMicad() {
	socketPath := "/tmp/mica/mica-create.socket"
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		fmt.Printf("⚠️  Warning: %s does not exist\n", socketPath)
		fmt.Println("   Please start mock_micad first:")
		fmt.Println("   cd tests/mock_micad && ./mock_micad")
		fmt.Println()
		fmt.Println("   Continuing anyway (tests will fail)...")
	} else {
		fmt.Printf("✅ Found mock_micad socket: %s\n", socketPath)
	}
	fmt.Println()
}

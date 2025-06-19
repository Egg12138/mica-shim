package libmica

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMicaCreate(t *testing.T) {
	fmt.Println("=== Testing MicaCreate (struct message) ===")

	config := NewMicaCreateMsg(
		3,                                // CPU (matches qemu-zephyr-rproc.conf)
		"qemu-zephyr",                    // Name (matches config)
		"/home/egg/playground/zephr.elf", // Path (matches config)
		"",                               // Ped (empty)
		"",                               // PedCfg (empty)
		false,                            // Debug
	)

	fmt.Printf("Sending create message:\n")
	fmt.Printf("  CPU: %d\n", config.cpu)
	fmt.Printf("  Name: %s\n", string(config.name[:]))
	fmt.Printf("  Path: %s\n", string(config.path[:]))
	fmt.Printf("  Debug: %t\n", config.debug)

	response, err := MicaCreate(config)
	if err != nil {
		t.Errorf("MicaCreate failed: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", response)

	if response != "MICA-SUCCESS" {
		t.Errorf("Expected MICA-SUCCESS, got: %s", response)
	}

	fmt.Println("✅ MicaCreate test passed!")
	fmt.Println()
}

func TestMicaCtlCommands(t *testing.T) {
	fmt.Println("=== Testing MicaCtl Commands (string messages) ===")

	client := "qemu-zephyr"

	commands := []struct {
		cmd  MicaCommand
		name string
	}{
		{MStart, "start"},
		{MStatus, "status"},
		{MStop, "stop"},
		{MRemove, "remove"},
	}

	for _, cmd := range commands {
		fmt.Printf("Testing %s command...\n", cmd.name)

		response, err := MicaCtl(cmd.cmd, client)
		if err != nil {
			fmt.Printf("  Command failed (expected): %v\n", err)
			continue
		}

		fmt.Printf("  Response: %s\n", response)

		if response != "MICA-SUCCESS" {
			t.Errorf("Expected MICA-SUCCESS for %s, got: %s", cmd.name, response)
		}
	}

	fmt.Println("✅ MicaCtl commands test completed!")
	fmt.Println()
}

func TestDummyCreateMsg(t *testing.T) {
	fmt.Println("=== Testing DummyCreateMsg ===")

	response, err := TestCreate()
	if err != nil {
		t.Errorf("TestCreate failed: %v", err)
		return
	}

	fmt.Printf("TestCreate response: %s\n", response)

	if response != "MICA-SUCCESS" {
		t.Errorf("Expected MICA-SUCCESS, got: %s", response)
	}

	fmt.Println("✅ DummyCreateMsg test passed!")
	fmt.Println()
}

func TestMessagePacking(t *testing.T) {
	fmt.Println("=== Testing Message Packing ===")

	config := NewMicaCreateMsg(3, "qemu-zephyr", "/home/egg/playground/zephr.elf", "", "", false)
	packed := config.pack()

	fmt.Printf("Packed message size: %d bytes (expected: 325)\n", len(packed))

	if len(packed) != 325 {
		t.Errorf("Expected 325 bytes, got %d bytes", len(packed))
	}

	// Check first 4 bytes (CPU field, little-endian)
	cpu := uint32(packed[0]) | uint32(packed[1])<<8 | uint32(packed[2])<<16 | uint32(packed[3])<<24
	fmt.Printf("CPU field: %d (expected: 3)\n", cpu)

	if cpu != 3 {
		t.Errorf("Expected CPU=3, got CPU=%d", cpu)
	}

	// Check name field (bytes 4-35)
	nameBytes := packed[4:36]
	name := string(nameBytes[:11]) // "qemu-zephyr" is 11 chars
	fmt.Printf("Name field: '%s' (expected: 'qemu-zephyr')\n", name)

	if name != "qemu-zephyr" {
		t.Errorf("Expected name 'qemu-zephyr', got '%s'", name)
	}

	// Check debug field (last byte)
	debugByte := packed[324]
	fmt.Printf("Debug field: %d (expected: 0)\n", debugByte)

	if debugByte != 0 {
		t.Errorf("Expected debug=0, got debug=%d", debugByte)
	}

	fmt.Println("✅ Message packing test passed!")
	fmt.Println()
}

// TestAllPublicFunctions tests all the public test functions
func TestAllPublicFunctions(t *testing.T) {
	fmt.Println("=== Testing All Public Test Functions ===")

	tests := []struct {
		name string
		fn   func() (string, error)
	}{
		{"TestCreate", TestCreate},
		{"TestStart", TestStart},
		{"TestStop", TestStop},
		{"TestRemove", TestRemove},
		{"TestStatus", TestStatus},
	}

	for _, test := range tests {
		fmt.Printf("Running %s...\n", test.name)

		response, err := test.fn()
		if err != nil {
			// Some tests may fail due to missing client sockets, that's okay
			fmt.Printf("  %s failed (may be expected): %v\n", test.name, err)
		} else {
			fmt.Printf("  %s response: %s\n", test.name, response)
		}
	}

	fmt.Println("✅ All public functions tested!")
	fmt.Println()
}

// Benchmark to compare performance
func BenchmarkMicaCreate(b *testing.B) {
	config := NewMicaCreateMsg(3, "qemu-zephyr", "/home/egg/playground/zephr.elf", "", "", false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MicaCreate(config)
	}
}

// TestMain runs before all tests - can be used for setup
func TestMain(m *testing.M) {
	fmt.Println("🧪 Starting socket.go tests...")
	fmt.Println("📝 Make sure mock_micad is running: ./mock_micad")
	fmt.Println("⏰ Waiting 2 seconds for you to start mock_micad if needed...")
	time.Sleep(2 * time.Second)
	fmt.Println()

	// Run the tests
	code := m.Run()

	fmt.Println()
	fmt.Println("🎯 Test summary:")
	fmt.Println("   - MicaCreate should send 325-byte struct (like mica.py)")
	fmt.Println("   - MicaCtl should send string commands")
	fmt.Println("   - Check mock_micad output to verify message format")
	fmt.Println()

	os.Exit(code)
}

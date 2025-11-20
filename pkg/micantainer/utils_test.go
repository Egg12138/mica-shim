//go:build test
// +build test

package micantainer

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	libmica "mica-shim/pkg/libmica"
	ped "mica-shim/pkg/pedestal"
)

// mirror of libmica.MicaClientConf for test access to cpuWeight.
// keep in sync with the real definition.
type micaClientConfMirror struct {
	name      [libmica.MaxNameLen]byte
	path      [libmica.MaxFirmwarePathLen]byte
	ped       [libmica.MaxNameLen]byte
	pedcfg    [libmica.MaxFirmwarePathLen]byte
	debug     bool
	cpuStr    [libmica.MaxCPUStringLen]byte
	vcpuNum   int
	cpuWeight int
	cpuCap    int
	memoryMB  int
	network   [libmica.MaxNetworkLen]byte
}

func cpuWeightFromConf(conf libmica.MicaClientConf) int {
	return (*micaClientConfMirror)(unsafe.Pointer(&conf)).cpuWeight
}

func memoryMBFromConf(conf libmica.MicaClientConf) int {
	return (*micaClientConfMirror)(unsafe.Pointer(&conf)).memoryMB
}

func TestCreateMicaClientConfUsesShareToWeight(t *testing.T) {
	oldPed := HostPedType
	HostPedType = ped.Xen
	defer func() { HostPedType = oldPed }()

	tests := []struct {
		name       string
		shares     uint64
		wantWeight int
	}{
		{
			name:       "zero share defaults",
			shares:     0,
			wantWeight: int(ped.ShareToWeight(0)),
		},
		{
			name:       "normal share scaled",
			shares:     2048,
			wantWeight: int(ped.ShareToWeight(2048)),
		},
		{
			name:       "tiny share clamps to one",
			shares:     1,
			wantWeight: int(ped.ShareToWeight(1)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			fwPath := filepath.Join(tempDir, "fw.elf")
			if err := os.WriteFile(fwPath, []byte("elf"), 0o600); err != nil {
				t.Fatalf("failed to create firmware fixture: %v", err)
			}

			container := &Container{
				id: tt.name,
				config: &ContainerConfig{
					CpuShares:    tt.shares,
					ElfAbsPath:   fwPath,
					PedestalConf: "image.bin",
				},
			}
			conf, err := createMicaClientConf(container)
			if err != nil {
				t.Fatalf("createMicaClientConf(%s) returned error: %v", tt.name, err)
			}
			if got := cpuWeightFromConf(conf); got != tt.wantWeight {
				t.Fatalf("cpu weight mismatch: got %d, want %d", got, tt.wantWeight)
			}
		})
	}
}

func TestCreateMicaClientConfUsesMemoryLimit(t *testing.T) {
	tempDir := t.TempDir()
	fwPath := filepath.Join(tempDir, "fw.elf")
	if err := os.WriteFile(fwPath, []byte("elf"), 0o600); err != nil {
		t.Fatalf("failed to create firmware fixture: %v", err)
	}

	container := &Container{
		id: "memory-limit",
		config: &ContainerConfig{
			MemoryLimitMB: 128,
			MemoryMinMB:   64,
			ElfAbsPath:    fwPath,
			PedestalConf:  "image.bin",
		},
	}

	conf, err := createMicaClientConf(container)
	if err != nil {
		t.Fatalf("createMicaClientConf returned error: %v", err)
	}
	if got := memoryMBFromConf(conf); got != 128 {
		t.Fatalf("MemoryMB = %d, want %d", got, 128)
	}
}

func TestCreateMicaClientConfFallsBackToMinWhenLimitUnset(t *testing.T) {
	tempDir := t.TempDir()
	fwPath := filepath.Join(tempDir, "fw.elf")
	if err := os.WriteFile(fwPath, []byte("elf"), 0o600); err != nil {
		t.Fatalf("failed to create firmware fixture: %v", err)
	}

	container := &Container{
		id: "memory-min",
		config: &ContainerConfig{
			MemoryLimitMB: 0,
			MemoryMinMB:   64,
			ElfAbsPath:    fwPath,
			PedestalConf:  "image.bin",
		},
	}

	conf, err := createMicaClientConf(container)
	if err != nil {
		t.Fatalf("createMicaClientConf returned error: %v", err)
	}
	if got := memoryMBFromConf(conf); got != 64 {
		t.Fatalf("MemoryMB fallback = %d, want %d", got, 64)
	}
}

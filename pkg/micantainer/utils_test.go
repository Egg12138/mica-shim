//go:build test
// +build test

package micantainer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	libmica "micrun/pkg/libmica"
	pedestal "micrun/pkg/pedestal"

	"github.com/opencontainers/runtime-spec/specs-go"
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
	HostPedType = pedestal.Xen
	defer func() { HostPedType = oldPed }()

	tests := []struct {
		name       string
		shares     uint64
		wantWeight int
	}{
		{
			name:       "zero share defaults",
			shares:     0,
			wantWeight: int(pedestal.ShareToWeight(0)),
		},
		{
			name:       "normal share scaled",
			shares:     2048,
			wantWeight: int(pedestal.ShareToWeight(2048)),
		},
		{
			name:       "tiny share clamps to one",
			shares:     1,
			wantWeight: int(pedestal.ShareToWeight(1)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			fwPath := filepath.Join(tempDir, "fw.elf")
			if err := os.WriteFile(fwPath, []byte("elf"), 0o600); err != nil {
				t.Fatalf("failed to create firmware fixture: %v", err)
			}

			shares := tt.shares
			container := &Container{
				id: tt.name,
				config: &ContainerConfig{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Shares: &shares,
						},
					},
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

	limitBytes := int64(128 * 1024 * 1024)
	container := &Container{
		id: "memory-limit",
		config: &ContainerConfig{
			Resources: &specs.LinuxResources{
				Memory: &specs.LinuxMemory{
					Limit: &limitBytes,
				},
			},
			ElfAbsPath:   fwPath,
			PedestalConf: "image.bin",
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

	reservationBytes := int64(64 * 1024 * 1024)
	container := &Container{
		id: "memory-min",
		config: &ContainerConfig{
			Resources: &specs.LinuxResources{
				Memory: &specs.LinuxMemory{
					Reservation: &reservationBytes,
				},
			},
			ElfAbsPath:   fwPath,
			PedestalConf: "image.bin",
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

func TestContainerUpdatePropagatesResourceValuesToExecutor(t *testing.T) {
	t.Parallel()

	quota := int64(200000)
	period := uint64(100000)
	shares := uint64(2048)
	memLimit := int64(512 * 1024 * 1024)

	sandbox := &Sandbox{
		state: SandboxState{State: StateRunning},
		config: &SandboxConfig{
			InfraOnly: true,
		},
	}

	exec := &fakeResourceExecutor{}

	container := &Container{
		id: "resource-update",
		config: &ContainerConfig{
			Resources: &specs.LinuxResources{
				CPU:    &specs.LinuxCPU{},
				Memory: &specs.LinuxMemory{},
			},
		},
		state:   ContainerState{State: StateRunning},
		sandbox: sandbox,
	}

	resources := specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			Quota:  &quota,
			Period: &period,
			Shares: &shares,
			Cpus:   "0-1",
		},
		Memory: &specs.LinuxMemory{
			Limit: &memLimit,
		},
	}

	if err := container.update(context.Background(), resources); err != nil {
		t.Fatalf("container.update returned error: %v", err)
	}

	expectedWeight := pedestal.ShareToWeight(shares)
	if exec.cpuWeight != expectedWeight {
		t.Fatalf("expected cpu weight %d, got %d", expectedWeight, exec.cpuWeight)
	}

	if exec.cpuSet != "0-1" {
		t.Fatalf("expected cpuset %q, got %q", "0-1", exec.cpuSet)
	}

	expectedMemMB := uint32(memLimit >> 20)
	if exec.memLimitMB != expectedMemMB {
		t.Fatalf("expected memory limit %d MiB, got %d MiB", expectedMemMB, exec.memLimitMB)
	}
}

type fakeResourceExecutor struct {
	cpuCapacity uint32
	memLimitMB  uint32
	cpuSet      string
	cpuWeight   uint32
	vcpu        uint32
}

func (f *fakeResourceExecutor) ReadResource() *pedestal.EssentialResource {
	res := pedestal.InitResource()
	if f.cpuCapacity > 0 {
		res.CpuCpacity = copyUint32(f.cpuCapacity)
	}

	if f.cpuWeight > 0 {
		weight := f.cpuWeight
		res.CPUWeight = &weight
	} else {
		res.CPUWeight = nil
	}

	if f.memLimitMB > 0 {
		res.MemoryMaxMB = copyUint32(f.memLimitMB)
	} else {
		res.MemoryMaxMB = nil
	}

	if f.vcpu > 0 {
		vcpu := f.vcpu
		res.Vcpu = &vcpu
	}

	res.ClientCpuSet = strings.TrimSpace(f.cpuSet)

	return res
}

func (f *fakeResourceExecutor) NeedUpdateCpuCap(target uint32) bool {
	return f.cpuCapacity != target
}

func (f *fakeResourceExecutor) UpdateCPUCapacity(cap uint32) error {
	f.cpuCapacity = cap
	return nil
}

func (f *fakeResourceExecutor) NeedUpdateMemLimit(target uint32) bool {
	return f.memLimitMB != target
}

func (f *fakeResourceExecutor) EnsureMemoryLimit(target uint32) error {
	f.memLimitMB = target
	return nil
}

func (f *fakeResourceExecutor) NeedUpdateCpuSet(old, new string) bool {
	current := strings.TrimSpace(f.cpuSet)
	if current != "" {
		return current != strings.TrimSpace(new)
	}
	return strings.TrimSpace(old) != strings.TrimSpace(new)
}

func (f *fakeResourceExecutor) UpdatePCPUConstrains(cpus string) error {
	f.cpuSet = cpus
	return nil
}

func (f *fakeResourceExecutor) NeedUpdateCpuShare(target uint32) bool {
	return f.cpuWeight != target
}

func (f *fakeResourceExecutor) UpdateCPUWeight(weight uint32) error {
	f.cpuWeight = weight
	return nil
}

func (f *fakeResourceExecutor) NeedUpdateVCpus(target uint32) bool {
	return f.vcpu != target
}

func (f *fakeResourceExecutor) UpdateVCPUNum(new uint32) (uint32, uint32, error) {
	old := f.vcpu
	f.vcpu = new
	return old, new, nil
}

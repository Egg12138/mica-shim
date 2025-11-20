//go:build test
// +build test

package micantainer

import (
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func TestContainerConfigSkipResourceParsingForInfra(t *testing.T) {
	quota := int64(1000)
	period := uint64(100000)
	memLimit := int64(512 * 1024 * 1024)

	spec := specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{
					Quota:  &quota,
					Period: &period,
				},
				Memory: &specs.LinuxMemory{
					Limit: &memLimit,
				},
			},
		},
	}

	cfg := ContainerConfig{
		ID:      "infra",
		IsInfra: true,
	}

	if err := cfg.ParseOCICPUResources(&spec); err != nil {
		t.Fatalf("ParseOCICPUResources returned error: %v", err)
	}
	if err := cfg.ParseOCIMemoryResources(&spec); err != nil {
		t.Fatalf("ParseOCIMemoryResources returned error: %v", err)
	}

	if cfg.CpuLimit != 0 || cfg.CpuQuota != 0 || cfg.CpuPeriod != 0 {
		t.Fatalf("expected CPU fields to remain zero for infra container, got %+v", cfg)
	}

	if cfg.MemoryLimitMB != 0 {
		t.Fatalf("expected memory limit to remain zero for infra container, got %d", cfg.MemoryLimitMB)
	}
}

func TestParseOCIMemoryReservationSetsMinimum(t *testing.T) {
	limit := int64(256 * 1024 * 1024)
	reservation := int64(64 * 1024 * 1024)
	spec := specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				Memory: &specs.LinuxMemory{
					Limit:       &limit,
					Reservation: &reservation,
				},
			},
		},
	}

	cfg := ContainerConfig{}
	if err := cfg.ParseOCIMemoryResources(&spec); err != nil {
		t.Fatalf("ParseOCIMemoryResources returned error: %v", err)
	}

	if cfg.MemoryReservationMB != uint32(reservation/1024/1024) {
		t.Fatalf("MemoryReservationMB = %d, want %d", cfg.MemoryReservationMB, reservation/1024/1024)
	}
	if cfg.MemoryMinMB != cfg.MemoryReservationMB {
		t.Fatalf("MemoryMinMB = %d, want %d", cfg.MemoryMinMB, cfg.MemoryReservationMB)
	}
}

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

	if cfg.Resources != nil {
		t.Fatalf("expected resources to remain nil for infra container, got %+v", cfg.Resources)
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

	if cfg.Resources == nil || cfg.Resources.Memory == nil || cfg.Resources.Memory.Reservation == nil {
		t.Fatalf("reservation not set in resources: %+v", cfg.Resources)
	}
	if got := *cfg.Resources.Memory.Reservation; got != reservation {
		t.Fatalf("memory reservation = %d, want %d", got, reservation)
	}
}

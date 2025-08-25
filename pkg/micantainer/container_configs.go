package micantainer

import (
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/libmica"

	"github.com/containerd/cgroups"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// ******* container configs ops *******

// stripQuotes removes surrounding quotes from a string if both start and end quotes match
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// parseOCICPUResources parses CPU resource limits from OCI spec
func (r *ContainerConfig) ParseOCICPUResources(spec *specs.Spec) error {
	if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.CPU == nil {
		return nil
	}

	cpu := spec.Linux.Resources.CPU

	// Parse CPU quota and period to get CPU limit
	if cpu.Quota != nil && cpu.Period != nil && *cpu.Period > 0 {
		r.CpuQuota = *cpu.Quota
		r.CpuPeriod = *cpu.Period
		cpuLimit := int(*cpu.Quota / int64(*cpu.Period))
		if cpuLimit > 0 {
			r.CpuLimit = cpuLimit
		}
	}

	if cpu.Shares != nil {
		r.CpuShares = *cpu.Shares
	}

	if cpu.Cpus != "" {
		r.CpusetCpus = cpu.Cpus
	}

	// Parse realtime CPU constraints if present
	if cpu.RealtimeRuntime != nil {
	}

	return nil
}

// parseOCIMemoryResources parses Memory resource limits from OCI spec
func (r *ContainerConfig) ParseOCIMemoryResources(spec *specs.Spec) error {
	if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.Memory == nil {
		log.Warn("No Memory resources specified in OCI spec")
		return nil
	}

	memory := spec.Linux.Resources.Memory

	if memory.Limit != nil {
		r.MemoryLimit = *memory.Limit
	}

	if memory.Reservation != nil {
		r.MemoryReservation = *memory.Reservation
	}

	if memory.Swap != nil {
		r.MemorySwap = *memory.Swap
	}

	// Deal with the deprecated field
	if cgroupV1() {
		if memory.Kernel != nil {
			r.MemoryKernel = *memory.Kernel
			log.Infof("Supported only in cgruopv1; parsed kernel memory limit: %d bytes", *memory.Kernel)
		}
	}

	if memory.Swappiness != nil {
		swappiness := uint64(*memory.Swappiness)
		r.MemorySwappiness = &swappiness
	}

	if memory.DisableOOMKiller != nil {
		r.OomKillDisable = *memory.DisableOOMKiller
	}

	return nil
}

func cgroupV1() bool {
	return cgroups.Mode() == cgroups.Legacy || cgroups.Mode() == cgroups.Hybrid
}

// validateResourceLimits validates container resource limits against system constraints
func ValidateResourceLimits(config *ContainerConfig) error {
	// Validate CPU limits
	if config.CpuLimit > 0 {
		systemCPUs := libmica.MaxClientCPUNum()
		if config.CpuLimit > systemCPUs {
			return fmt.Errorf("container CPU limit %d exceeds system CPU count %d", config.CpuLimit, systemCPUs)
		}
	}

	// Validate memory limits
	if config.MemoryLimit > 0 {
		systemMemory := getSystemMemoryBytes()
		if config.MemoryLimit > systemMemory {
			return fmt.Errorf("container memory limit %d bytes exceeds system memory %d bytes", config.MemoryLimit, systemMemory)
		}
	}

	// Validate memory swappiness
	if config.MemorySwappiness != nil && *config.MemorySwappiness > 100 {
		return fmt.Errorf("invalid memory swappiness value %d, must be 0-100", *config.MemorySwappiness)
	}

	// Validate CPU period constraints (from Linux kernel documentation)
	if config.CpuPeriod > 0 && (config.CpuPeriod < 1000 || config.CpuPeriod > 1000000) {
		return fmt.Errorf("invalid CPU period %d, must be between 1000 and 1000000 microseconds", config.CpuPeriod)
	}

	// Validate CPU quota constraints
	if config.CpuQuota > 0 && config.CpuPeriod > 0 && config.CpuQuota < 1000 {
		return fmt.Errorf("invalid CPU quota %d, must be at least 1000 microseconds", config.CpuQuota)
	}

	return nil
}

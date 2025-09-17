package micantainer

import (
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/libmica"
	"mica-shim/pkg/pedestal"

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

	essentialRes := pedestal.LinuxResource2Essential(spec)
	r.CpuLimit = int(*essentialRes.CpuCpacity)
	r.CpuPeriod = *essentialRes.CpuPeriod
	r.CpuQuota = *essentialRes.CpuQuota
	r.CpuShares = uint64(*essentialRes.CPUWeight)
	r.VCPUNum = int(*essentialRes.Vcpu)
	r.CpusetCpus = essentialRes.ClientCpuSet
	r.MemoryLimit = int64(*essentialRes.MemoryLimitMB)
	log.Debugf(`
		EssentialResource:
		CpuLimit = %d
		CpuPeriod = %d
		CpuQuota = %d
		CpuShares = %d
		VPUNum = %d
		CpusetCpus = %s
		MemoryLimit = %d
	}
	`, r.CpuLimit, r.CpuPeriod, r.CpuQuota, r.CpuShares, r.VCPUNum, r.CpusetCpus, r.MemoryLimit)
	

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
		r.MemoryLimit = int64(*memory.Limit / 1024 / 1024)
	}

	if memory.Reservation != nil {
		r.MemoryReservation = int64(*memory.Reservation / 1024 / 1024)
	}

	if memory.Swap != nil {
		r.MemorySwap = int64(*memory.Swap / 1024 / 1024)
	}

	if memory.Swappiness != nil {
		swappiness := uint64(*memory.Swappiness / 1024 / 1024)
		r.MemorySwappiness = &swappiness
	}

	if memory.DisableOOMKiller != nil {
		r.OomKillDisable = *memory.DisableOOMKiller
	}

	return nil
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

	return nil
}

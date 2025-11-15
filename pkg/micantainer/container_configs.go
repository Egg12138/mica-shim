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
	if r.IsInfra {
		return nil
	}
	if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.CPU == nil {
		return nil
	}

	essentialRes := pedestal.PlanEssentialResources(spec)
	r.CpuLimit = *essentialRes.CpuCpacity
	// Only copy CPU period/quota/shares when explicitly specified and non-zero
	if cpu := spec.Linux.Resources.CPU; cpu != nil {
		if cpu.Period != nil && *cpu.Period > 0 {
			r.CpuPeriod = *cpu.Period
		}
		if cpu.Quota != nil && *cpu.Quota > 0 {
			r.CpuQuota = *cpu.Quota
		}
		if cpu.Shares != nil && *cpu.Shares > 0 {
			r.CpuShares = *cpu.Shares
		}
	}
	// Copy vCPU and cpuset only when meaningful
	if essentialRes.Vcpu != nil && *essentialRes.Vcpu > 0 {
		r.VCPUNum = *essentialRes.Vcpu
	}
	if essentialRes.ClientCpuSet != "" {
		r.CpusetCpus = essentialRes.ClientCpuSet
	}

	// Validate cpuset ranges against host max CPUs; adjust if needed.
	if r.CpusetCpus != "" {
		cpus, err := libmica.ParseCPUString(r.CpusetCpus)
		if err == nil {
			if ok, out := CpusetRangeValid(cpus); !ok {
				// Filter out-of-range CPUs and rebuild cpuset string.
				valid := make([]int, 0, len(cpus))
				bad := map[int]struct{}{}
				for _, x := range out {
					bad[x] = struct{}{}
				}
				for _, x := range cpus {
					if _, miss := bad[x]; !miss {
						valid = append(valid, x)
					}
				}
				if len(valid) > 0 {
					r.CpusetCpus = pedestal.ParseCPUArr(valid)
					r.VCPUNum = uint32(len(valid))
				} else {
					// All invalid; clear cpuset and keep a sane default for VCPUs.
					r.CpusetCpus = ""
					r.VCPUNum = 1
				}
			}
		}
	}
	// Memory limit is parsed in ParseOCIMemoryResources; ignore defaults/unspecified here
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
	`, r.CpuLimit, r.CpuPeriod, r.CpuQuota, r.CpuShares, r.VCPUNum, r.CpusetCpus, r.MemoryLimitMB)

	return nil
}

// parseOCIMemoryResources parses Memory resource limits from OCI spec
func (r *ContainerConfig) ParseOCIMemoryResources(spec *specs.Spec) error {
	if r.IsInfra {
		return nil
	}
	if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.Memory == nil {
		log.Warn("No Memory resources specified in OCI spec")
		return nil
	}

	memory := spec.Linux.Resources.Memory

	if memory.Limit != nil && *memory.Limit > 0 {
		r.MemoryLimitMB = uint32(*memory.Limit / 1024 / 1024)
	}

	if memory.Reservation != nil && *memory.Reservation > 0 {
		r.MemoryReservationMB = uint32(*memory.Reservation / 1024 / 1024)
	}

	if memory.Swap != nil && *memory.Swap > 0 {
		r.MemorySwapMB = uint32(*memory.Swap / 1024 / 1024)
	}

	if memory.Swappiness != nil && *memory.Swappiness > 0 {
		swappiness := uint32(*memory.Swappiness)
		r.MemorySwappinessMB = &swappiness
	}

	if memory.DisableOOMKiller != nil {
		r.OomKillDisable = *memory.DisableOOMKiller
	}

	return nil
}

// validateResourceLimits validates container resource limits against system constraints
func ValidateResourceLimits(config *ContainerConfig) error {
	if config.IsInfra {
		return nil
	}
	// Validate CPU limits
	if config.CpuLimit > 0 {
		systemCPUs := libmica.MaxClientCPUNum()
		if int(config.CpuLimit) > systemCPUs {
			return fmt.Errorf("container CPU limit %d exceeds system CPU count %d", config.CpuLimit, systemCPUs)
		}
	}

	// Validate memory limits
	if config.MemoryLimitMB > 0 {
		mem := pedestal.HostMemoryMiB()
		hostMemMB := mem.TotalMB
		if hostMemMB == 0 {
			log.Warn("Failed to detect host memory, using fallback value: 2 GiB")
			hostMemMB = 2 * 1024 // Fallback to 2GiB when detection fails.
		}
		if config.MemoryLimitMB > hostMemMB {
			return fmt.Errorf("container memory limit %d MiB exceeds system memory %d MiB", config.MemoryLimitMB, hostMemMB)
		}
	}

	return nil
}

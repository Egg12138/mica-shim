package micantainer

import (
	"fmt"
	log "micrun/logger"
	"micrun/pkg/libmica"
	"micrun/pkg/pedestal"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// ******* container configs ops *******

// ParseOCIResources parses both CPU and Memory resource limits from OCI spec in a single pass
func (r *ContainerConfig) ParseOCIResources(spec *specs.Spec) error {
	if r.IsInfra {
		return nil
	}

	r.ensureResources()

	essentialRes := pedestal.PlanEssentialResources(spec)

	if spec.Linux != nil && spec.Linux.Resources != nil && spec.Linux.Resources.CPU != nil {
		r.Resources.CPU = cloneLinuxCPU(spec.Linux.Resources.CPU)

		if essentialRes.Vcpu != nil && *essentialRes.Vcpu > 0 {
			r.VCPUNum = *essentialRes.Vcpu
		}
		if essentialRes.ClientCpuSet != "" {
			if cpu := r.Resources.CPU; cpu != nil {
				cpu.Cpus = essentialRes.ClientCpuSet
			}
		}

		// TODO: need to reuse cpuset package function
		if cpu := r.Resources.CPU; cpu != nil && cpu.Cpus != "" {
			cpus, err := libmica.ParseCPUString(cpu.Cpus)
			if err == nil {
				if ok, out := CpusetRangeValid(cpus); !ok {
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
						cpu.Cpus = pedestal.ParseCPUArr(valid)
						r.VCPUNum = uint32(len(valid))
					} else {
						// All invalid; clear cpuset and keep a sane default for VCPUs.
						cpu.Cpus = ""
						r.VCPUNum = 1
					}
				}
			}
		}

		cpu := r.Resources.CPU
		var sharesVal uint64
		var cpusetVal string
		if cpu != nil {
			if cpu.Shares != nil {
				sharesVal = *cpu.Shares
			}
			cpusetVal = cpu.Cpus
		}
		log.Debugf(`
			EssentialResource:
			CpuCapacity = %d
			CpuShares = %d
			VCPUNum = %d
			CpusetCpus = %s
			MemoryLimit = %d
		}
		`, r.cpuCapacity(), sharesVal, r.VCPUNum, cpusetVal, r.memoryLimitMB())
	}

	if spec.Linux != nil && spec.Linux.Resources != nil && spec.Linux.Resources.Memory != nil {
		r.Resources.Memory = cloneLinuxMemory(spec.Linux.Resources.Memory)
	} else {
		log.Warn("No Memory resources specified in OCI spec")
	}

	return nil
}

func cloneLinuxCPU(src *specs.LinuxCPU) *specs.LinuxCPU {
	if src == nil {
		return &specs.LinuxCPU{}
	}
	return &specs.LinuxCPU{
		Shares:          copyUint64(src.Shares),
		Quota:           copyInt64(src.Quota),
		Burst:           copyUint64(src.Burst),
		Period:          copyUint64(src.Period),
		RealtimeRuntime: copyInt64(src.RealtimeRuntime),
		RealtimePeriod:  copyUint64(src.RealtimePeriod),
		Cpus:            src.Cpus,
		Mems:            src.Mems,
		Idle:            copyInt64(src.Idle),
	}
}

func cloneLinuxMemory(src *specs.LinuxMemory) *specs.LinuxMemory {
	if src == nil {
		return &specs.LinuxMemory{}
	}

	return &specs.LinuxMemory{
		Limit:            copyInt64(src.Limit),
		Reservation:      copyInt64(src.Reservation),
		Swap:             copyInt64(src.Swap),
		Swappiness:       copyUint64(src.Swappiness),
		DisableOOMKiller: copyBool(src.DisableOOMKiller),
	}
}

func copyInt64(src *int64) *int64 {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}

func copyUint64(src *uint64) *uint64 {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}

func copyBool(src *bool) *bool {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}

// validateResourceLimits validates container resource limits against system constraints
func ValidateResourceLimits(config *ContainerConfig) error {
	if config.IsInfra {
		return nil
	}
	// Validate CPU limits
	if cpuLimit := config.cpuCapacity(); cpuLimit > 0 {
		systemCPUs := libmica.MaxClientCPUNum()
		if int(cpuLimit) > systemCPUs {
			return fmt.Errorf("container CPU limit %d exceeds system CPU count %d", cpuLimit, systemCPUs)
		}
	}

	// Validate memory limits
	if limit := config.memoryLimitMB(); limit > 0 {
		mem := pedestal.HostMemoryMiB()
		hostMemMB := mem.TotalMB
		if hostMemMB == 0 {
			log.Warn("Failed to detect host memory, using fallback value: 2 GiB")
			hostMemMB = 2 * 1024 // Fallback to 2GiB when detection fails.
		}
		if limit > hostMemMB {
			return fmt.Errorf("container memory limit %d MiB exceeds system memory %d MiB", limit, hostMemMB)
		}
	}

	return nil
}

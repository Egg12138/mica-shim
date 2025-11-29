package pedestal

import (
	log "micrun/logger"
	"micrun/pkg/cpuset"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type resourcePlanner interface {
	FromSpec(spec *specs.Spec) *EssentialResource
}

// PlanEssentialResources returns the essential resource view for the current host pedestal.
func PlanEssentialResources(spec *specs.Spec) *EssentialResource {
	if spec == nil || spec.Linux == nil || spec.Linux.Resources == nil {
		return InitResource()
	}
	return plannerForHost().FromSpec(spec)
}

// LinuxResource2Essential is kept for backward compatibility; new code should call PlanEssentialResources.
func LinuxResource2Essential(spec *specs.Spec) *EssentialResource {
	return PlanEssentialResources(spec)
}

func plannerForHost() resourcePlanner {
	switch GetHostPed() {
	case Xen:
		return xenPlanner{}
	default:
		return defaultPlanner{}
	}
}

type xenPlanner struct{}

func (xenPlanner) FromSpec(spec *specs.Spec) *EssentialResource {
	return linuxResourceToEssential(spec, true)
}

type defaultPlanner struct{}

func (defaultPlanner) FromSpec(spec *specs.Spec) *EssentialResource {
	return linuxResourceToEssential(spec, false)
}

func linuxResourceToEssential(spec *specs.Spec, convertShares bool) *EssentialResource {
	res := InitResource()
	if spec == nil || spec.Linux == nil || spec.Linux.Resources == nil {
		return res
	}

	if cpu := spec.Linux.Resources.CPU; cpu != nil {
		if cpu.Quota != nil && *cpu.Quota > 0 && cpu.Period != nil && *cpu.Period > 0 {
			res.CpuPeriod = cpu.Period
			res.CpuQuota = cpu.Quota
			cpuCapacity := *cpu.Quota / int64(*cpu.Period)
			if cpuCapacity > 0 {
				*res.CpuCpacity = uint32(100 * cpuCapacity)
			}
		} else {
			log.Debugf("cpu quota/period pair = < %v:%v > is incomplete", cpu.Quota, cpu.Period)
		}

		if cpu.Shares != nil && *cpu.Shares > 0 {
			if convertShares {
				weight := ShareToWeight(*cpu.Shares)
				res.CPUWeight = &weight
			} else {
				share := uint32(*cpu.Shares)
				res.CPUWeight = &share
			}
		} else if convertShares {
			weight := uint32(DefaultXenWeight)
			res.CPUWeight = &weight
		} else {
			res.CPUWeight = nil
		}

		cpus, vcpuNum := validateCPUSet(cpu.Cpus)
		if cpus != "" && vcpuNum > 0 {
			res.ClientCpuSet = cpus
		}

		if vcpuNum > 0 {
			*res.Vcpu = uint32(vcpuNum)
		}
	} else {
		res.CPUWeight = nil
	}

	if mem := spec.Linux.Resources.Memory; mem != nil && mem.Limit != nil && *mem.Limit > 0 {
		*res.MemoryMaxMB = uint32(*mem.Limit >> 20)
	}

	return res
}

func validateCPUSet(s string) (validSet string, vcpus uint32) {
	set, err := cpuset.Parse(s)
	if err != nil {
		return "", 0
	}
	validSet = s
	return validSet, uint32(set.Size())
}

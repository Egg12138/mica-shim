// reference: kata
package oci

import (
	log "mica-shim/logger"
	"mica-shim/pkg/utils"
	"strconv"

	ctrAnnotations "github.com/containerd/containerd/pkg/cri/annotations"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func CalculateSandboxSizing(spec *specs.Spec) (uint32, uint32) {
	var memory, quota int64
	var period uint64
	var err error

	if spec == nil || spec.Annotations == nil {
		return 0, 0
	}

	annotation, ok := spec.Annotations[ctrAnnotations.SandboxCPUPeriod]
	if ok {
		period, err = strconv.ParseUint(annotation, 10, 64)
		if err != nil {
			log.Debugf("failure to parse SandboxCPUPeriod: %s", annotation)
			period = 0
		}
	}

	annotation, ok = spec.Annotations[ctrAnnotations.SandboxCPUQuota]
	if ok {
		quota, err = strconv.ParseInt(annotation, 10, 64)
		if err != nil {
			log.Debugf("failure to parse SandboxCPUQuota: %s", annotation)
			quota = 0
		}
	}

	annotation, ok = spec.Annotations[ctrAnnotations.SandboxMem]
	if ok {
		memory, err = strconv.ParseInt(annotation, 10, 64)
		if err != nil {
			log.Debugf("failure to parse SandboxMem: %s", annotation)
			memory = 0
		}
	}

	return clientInitResources(period, quota, memory)
}

// CalculateContainerSizing calculates the number of CPUs and amount of memory that is needed
// based on the provided LinuxResources.
func CalculateContainerSizing(spec *specs.Spec) (numCPU, memSizeMB uint32) {
	var memory, quota int64
	var period uint64

	if spec == nil || spec.Linux == nil || spec.Linux.Resources == nil {
		return 0, 0
	}
	resources := spec.Linux.Resources

	if resources.CPU != nil && resources.CPU.Quota != nil && resources.CPU.Period != nil {
		quota = *resources.CPU.Quota
		period = *resources.CPU.Period
	}

	if resources.Memory != nil && resources.Memory.Limit != nil {
		memory = *resources.Memory.Limit
	}

	return clientInitResources(period, quota, memory)
}

// NOTICE: mica now required Memory in MB
// clientInitResources calculates real resource count for client initialization.
func clientInitResources(period uint64, quota int64, memory int64) (numCPU, memSizeMB uint32) {
	// cooredinate with CPU calculating capacity
	numCPU = utils.CalculateVCpusFromMilliCpus(utils.CalculateMilliCPUs(quota, period))

	if memory < 0 {
		// While spec allows for a negative value to indicate unconstrained, we don't
		// see this in practice. Since we rely only on default memory if the workload
		// is unconstrained, we will treat as 0 for VM resource accounting.
		log.Debugf("memory limit provided < 0, treating as 0 MB for VM sizing: %d", memory)
		memSizeMB = 0
	} else {
		memSizeMB = uint32(memory / 1024 / 1024)
	}
	return numCPU, memSizeMB
}

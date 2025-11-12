//go:build debug
// +build debug

package libmica

import (
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/pedestal"
	"strconv"
	"strings"
)

// handleMicaUpdateWithXl handles MUpdate commands using xl commands instead of micad set command
func handleMicaUpdateWithXl(id string, opts ...string) error {
	if len(opts) < 1 {
		return fmt.Errorf("update command requires at least 1 parameter: resourceType and optional value")
	}

	// arguments string (e.g., "VCPU 4", "CPUWeight 256", "CPU 1-3")
	var resourceType, value string
	if len(opts) == 1 {
		parts := strings.Fields(opts[0])
		if len(parts) < 1 {
			return fmt.Errorf("invalid command format: %s", opts[0])
		}
		resourceType = parts[0]
		if len(parts) > 1 {
			value = strings.Join(parts[1:], " ")
		}
	} else {
		resourceType = opts[0]
		if len(opts) > 1 {
			value = strings.Join(opts[1:], " ")
		}
	}

	if pedestal.GetHostPed() != pedestal.Xen {
		return fmt.Errorf("xl command workaround only supported on Xen pedestal")
	}

	switch resourceType {
	case "Memory":
		memMB, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid memory value %s: %v", value, err)
		}
		return pedestal.XlMemSet(id, memMB)

	case "MaxMem":
		memMB, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid max memory value %s: %v", value, err)
		}
		return pedestal.XlMemMax(id, memMB)

	case "CPUWeight":
		weight, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid CPU weight value %s: %v", value, err)
		}
		if weight < 1 {
			log.Debugf("CPU weight must be >= 1, got %d, set to default 256", weight)
			weight = 256
		}
		return pedestal.XlSchedCredit2(id, weight, 0)

	case "CPUCpacity":
		capacity, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid CPU capacity value %s: %v", value, err)
		}
		return pedestal.XlSchedCredit2(id, 0, capacity)

	case "VCPU":
		vcpuCount, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid VCPU count value %s: %v", value, err)
		}
		return pedestal.XlVcpuSet(id, vcpuCount)

	case "CPU":
		log.Infof("PCPUConstrain (%s) temporarily not implemented, returning success", value)
		return nil

	default:
		return fmt.Errorf("unsupported resource type %s for xl command workaround", resourceType)
	}
}

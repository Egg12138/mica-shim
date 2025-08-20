package libmica

import (
	defs "mica-shim/definitions"
	ped "mica-shim/pkg/pedestal"
	"runtime"
	"strings"
)

// Helper functions
func startWithMicaPrefix(fieldName string) bool {
	return strings.HasPrefix(fieldName, defs.MicraLabelPrefix)
}

func isMicaAnnotation(fieldName string) string {
	return strings.TrimPrefix(fieldName, defs.MicraLabelPrefix)
}

func MaxCPUNum() int {
	pedtype := ped.HostPed()
	if defs.IsMock {
		return dummyCPUNum()
	}
	if pedtype == ped.Xen {
		return int(ped.MaxCPUNum())
	}
	return dummyCPUNum()
}

func dummyCPUNum() int {
	return runtime.NumCPU()
}
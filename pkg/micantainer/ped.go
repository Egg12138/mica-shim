package micantainer

import (
	"mica-shim/pkg/libmica"
	"runtime"
)

func machineCPUNumber() uint32 {
	return uint32(libmica.MaxCPUNum())
}

func maxVCPUNumber() uint32 {
	return uint32(runtime.NumCPU())
}

func machineMemoryMB() uint32 {
	return libmica.MaxMemMB()
}

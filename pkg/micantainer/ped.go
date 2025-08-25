package micantainer

import (
	"mica-shim/pkg/libmica"
	"runtime"
)

func maxCPUNumber() uint32 {
	return uint32(libmica.MaxCPUNum())
}

// TODO: 查清楚 xl 如何查询 vcpu， 以及对应的值的意义
func maxVCPUNumber() uint32 {
	return uint32(runtime.NumCPU())
}

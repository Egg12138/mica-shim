package micantainer

import "runtime"

type PedConfig struct {
}


func GetMaxXenCPU() uint32 {
	// TODO: runtime can not detect max CPU numbers Xen can handels
	systemCPUs := runtime.NumCPU()
	return uint32(systemCPUs)
}
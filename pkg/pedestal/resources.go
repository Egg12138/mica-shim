package pedestal

// EssentialResource contains essential resource specifications for a client
type EssentialResource struct {
	CpuQuota      int64
	CpuPeriod     uint64
	// mica conf: CPUCapacity
	CpuCpacity    uint32
	// mica conf: CPUWeight
	CPUWeight     uint32
	// mica conf: CPU
	ClientCpuSet string
	// mica conf: vcpu
	Vcpu          uint32
	// mica conf: Memory
	MemoryLimit   uint32
}
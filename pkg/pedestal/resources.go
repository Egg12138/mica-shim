package pedestal

// EssentialResource contains essential resource specifications for a client
type EssentialResource struct {
	CpuQuota      int64
	CpuPeriod     uint64
	// mica conf: CPUCapacity
	CpuCpacity    uint32
	// mica conf: CPUWeight
	CPUWeight     uint32
	// mica conf: CPU, representing CpuAffinity []i32
	ClientCpuSet  string 
	// mica conf: vcpu
	Vcpu          uint32
	// mica conf: Memory 
	// for Xen: DomU start with 32MB memory without memory option, but errored when execeeded MemoryLimit
	MemoryLimit   uint32

	// the initial memory for DomU client, default to be 32MiB. 
	// Xen needs Ballon Driver kernel module(xen-ballon.ko) to support memory ballooning.
	MemoryMin     uint32
	// Virtual network interface
	VIF           []string
}
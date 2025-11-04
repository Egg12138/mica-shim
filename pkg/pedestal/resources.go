package pedestal

// EssentialResource contains essential resource specifications for a client
type EssentialResource struct {
	CpuQuota  *int64
	CpuPeriod *uint64
	// mica conf: CPUCapacity,
	CpuCpacity *uint32
	// mica conf: CPUWeight
	CPUWeight *uint32
	// mica conf: CPU, representing CpuAffinity []i32
	ClientCpuSet string
	// mica conf: vcpu
	Vcpu *uint32
	// mica conf: Memory
	// for Xen: DomU start with 32MB memory without memory option, but errored when execeeded MemoryLimitMB
	MemoryLimitMB *uint32

	// the initial memory for DomU client, default to be 32MiB.
	// Xen needs Ballon Driver kernel module(xen-ballon.ko) to support memory ballooning.
	MemoryMinMB uint32
	// Virtual network interface
	VIF []string
}

// default value of essential resource struct, not about runtime config
const (
	defaultPeriod = 10000
	defaultQuota  = 0
	defaultVcpus  = 1
)

func InitResource() *EssentialResource {
	res := EssentialResource{}
	period := uint64(defaultPeriod)
	quota := int64(defaultQuota)
	vcpu := uint32(defaultVcpus)
	capacity := uint32(0)
	memLimit := uint32(32)
	res.CpuPeriod = &period
	res.CpuQuota = &quota
	res.Vcpu = &vcpu
	res.CpuCpacity = &capacity
	res.MemoryLimitMB = &memLimit
	res.MemoryMinMB = 32 // Default minimum memory for DomU
	return &res
}

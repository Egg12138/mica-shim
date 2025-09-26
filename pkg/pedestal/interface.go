package pedestal

// TODO: use interface to handle so many different pedestal
type PedTraits interface {
	ToString() string
	GeneratePedConf() (PedConfigString, error)
	// only support pinning all vcpu to another cpuset
	PinVCPU(shortId, cpus string)
	MemLowThreshold() uint32
	MemHighThreshold() uint32
}
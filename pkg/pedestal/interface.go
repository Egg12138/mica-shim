package pedestal

// TODO: use interface to handle so many different pedestal
type PedTraits interface {
	ToString() string
	GeneratePedConf() (PedConfig, error)
	// only support pinning all vcpu to another cpuset
	PinVCPU(shortId, cpus string)
}
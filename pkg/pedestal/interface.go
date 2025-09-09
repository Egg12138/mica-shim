package pedestal

// TODO: use interface to handle so many different pedestal
type PedTraits interface {
	ToString() string
	GeneratePedConf() (PedConfig, error)
}
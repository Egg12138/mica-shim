package defs

// Client default values.
const (
	DefaultNcpu = 1
	// fallback default mica configuration file
	DefaultClientConf = "client.conf"
	// pass "<bundle>/rootfs/<DefaultXenBin>" to pedestalCfg for xen-mica case
	DefaultXenBin = "image.bin"
)

// client conf keys
const (
	ElfPath = "path"
	Name    = "name"
	Ped     = "ped"
	PedCfg  = "pedcfg"
	Dbg     = "debug"
	Mem     = "memory"
	Weight  = "cpuweight"
	Cpus    = "cpustr"
	Cap     = "cpucapacity"
	Net     = "network"
)

var (
	PreservedOS   = [...]string{"zephyr", "uniproton", "linux"}
	OKSectionList = [...]string{"mica", "more"}
)

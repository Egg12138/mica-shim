package defs

// Configuration for mica clients
const (
	DefaultNcpu          int = 1
	MicaAnnotationPrefix     = "org.openeuler.mica" // used for runtime-level configurtaion
	MicaLabelPrefix          = "io.mica"

	DefaultClientConf = "client.conf"
	// ini config keys in [Mica] section of client.conf
	OS           = "os"
	Firmware     = "clientpath"
	Pedestal     = "pedestal"
	PedestalConf = "pedestalconf"
	Ncpu         = "ncpu"
	Compat       = "compatibility"

	// these items in client.conf will be ignored: CPU, Name, AutoBoot, Debug,
	// runtime will configure these items by container logic
	// AutoBoot       = "autoboot"
	// Debug          = "debug"
	// CPU            = "cpu"
	// Name           = "name"

)

var (
	PreservedOS   = [...]string{"zephyr", "uniproton", "linux"}
	OKSectionList = [...]string{"mica", "more"}
)

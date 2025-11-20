package defs

// Client default values.
const (
	// pass "<bundle>/rootfs/<DefaultXenBin>" to pedestalCfg for xen-mica case
	// all these default values should be in configuration
	DefaultXenBin       = "image.bin"
	DefaultFirmwareName = "firmware.elf"
	DefaultMinMemMB     = 16
)

var (
	PreservedOS = [...]string{"zephyr", "uniproton", "linux"}
)

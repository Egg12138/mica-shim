package defs

// Client default values.
const (
	// pass "<bundle>/rootfs/<DefaultXenBin>" to pedestalCfg for xen-mica case
	DefaultXenBin       = "image.bin"
	DefaultFirmwareName = "firmware.elf"
	DefaultMinMemMB     = 4
)

var (
	PreservedOS = [...]string{"zephyr", "uniproton", "linux"}
)

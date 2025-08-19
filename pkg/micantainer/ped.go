package micantainer

import (
	"mica-shim/pkg/libmica"
	"runtime"
	"strings"
)

type PedType int
type PedConfig string

const (
	Xen PedType = iota
	FusionDock
	PVE
	ACRN
	Unsupported
)

// String returns the string representation of PedType
func (p PedType) String() string {
	switch p {
	case Xen:
		return "xen"
	default:
		return "unknown"
	}
}

func ParsePedType(s string) PedType {
	switch strings.ToLower(s) {
	case "xen", "":
		return Xen
	default:
		return Unsupported // default to baremetal
	}
}

func maxCPUNumber() uint32 {
	return libmica.MaxCPUNum()
}

// TODO: 查清楚 xl 如何查询 vcpu， 以及对应的值的意义
func maxVCPUNumber() uint32 {
	return uint32(runtime.NumCPU())
}

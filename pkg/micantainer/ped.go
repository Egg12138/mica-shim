package micantainer

import "strings"

type PedType int

const (
	Xen PedType = iota
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
	return GetMaxXenCPU()
}
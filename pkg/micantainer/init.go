package micantainer

import (
	"sync"

	log "mica-shim/logger"
	ped "mica-shim/pkg/pedestal"
)

var (
	HostPedType ped.PedType
	HostPedOnce sync.Once
)

func init() {
	if HostPedType == ped.Unsupported {
		log.Warnf("unsupported host ped type")
	}
}


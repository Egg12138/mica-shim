package micantainer

import (
	log "mica-shim/logger"
	ped "mica-shim/pkg/pedestal"
)

var HostPedType ped.PedType

func init() {
	HostPedType = ped.HostPed()
	if HostPedType == ped.Unsupported {
		log.Warnf("unsupported host pedestal type")
	}
}

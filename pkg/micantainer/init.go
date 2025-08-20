package micantainer

import (
	log "mica-shim/logger"
	ped "mica-shim/pkg/pedestal"
)

var HostPedType ped.PedType

func init() {
	HostPedType = ped.HostPed()
	log.Debugf("detected host pedestal type: %s", HostPedType.String())
	if HostPedType == ped.Unsupported {
		log.Warnf("unsupported host pedestal type")
	}
}

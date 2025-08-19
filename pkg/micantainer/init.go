package micantainer

import log "mica-shim/logger"

var HostPedType PedType

func init() {
	HostPedType = HostPed()
	log.Debugf("detected host pedestal type: %s", string(HostPedType))
	if HostPedType == Unsupported {
		log.Warnf("unsupported host pedestal type")
	}
}

package cntr

import (
	log "mica-shim/logger"
)

var HostPedestalType PedType
var HostMaxCPU int

// Some crutial information should be shared between shim instances!
func init() {

	HostPedestalType = hostPed()
	if HostPedestalType == Unknown {
		log.Infof(`unknown pedestal type. The host is detected 
		as multi-pedestal mixture or lack of pedestal components, which is not supported yet.
		Please clear the host build cache and try again.`)
	}
	HostMaxCPU = availableMaxCPU()
	log.Debugf("cntr init: HostPedestalType: %v, HostMaxCPU: %d", HostPedestalType, HostMaxCPU)
}

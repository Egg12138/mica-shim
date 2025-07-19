package cntr

import (
	log "mica-shim/logger"
)

var HostPedestalType PedType
var HostMaxCPU int

// Some crutial information should be shared between shim instances!
func init() {
	log.Debugf("cntr init")

	HostPedestalType = hostPed()
	if HostPedestalType == Unknown {
		log.Warnf(`unknown pedestal type. The host is detected 
		as multi-pedestal mixture, which is not supported yet.
		Please clear the host build cache and try again.`)
	}
	HostMaxCPU = availableMaxCPU()
}

package cntr

import (
	log "mica-shim/logger"
)

var HostPedestalType PedType
var HostMaxCPU int

func init() {
	log.LocateDebugf("cntr init")
	HostPedestalType = hostPed()
	HostMaxCPU = availableMaxCPU()

}

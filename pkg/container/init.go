package cntr

var HostPedestalType PedType
var HostMaxCPU int

// Some crutial information should be shared between shim instances!
func init() {
	if HostPedestalType == Unsupported {
		HostPedestalType = hostPed()
		HostMaxCPU = availableMaxCPU()
	}
}

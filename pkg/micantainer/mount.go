package micantainer

// Host filesystem mounting will be implemented well in future
type Mount struct {
	Type    string
	Source  string
	Target  string
	// the mounting destination inside the Client RTOS; 
	MountDestInClient string
	// blkdev is the block device id attached to the Mica client
	// Currently, MICA is incapable of mounting block devices
	BlockDeviceID string
	Options []string
	ReadOnly  bool
}
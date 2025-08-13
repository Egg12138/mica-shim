package libmica

// For different RTOS, we have different error codes
// we will establish a mapping between error codes and POSIX err messages

const (
	OSs     = "zephyr,freertos,uniproton"
)

type PosixCompatity int

const (
	POSIX_PARTIAL_COMPATITY PosixCompatity = iota
	POSIX_FULL_COMPATITY
	POSIX_NOT_COMPATITY
)



var (
	POSIX_COMPATITIES = map[string]PosixCompatity{
		"zephyr": POSIX_PARTIAL_COMPATITY,
	}
)



func ConvertToPosixError(os string, err int) int32 {
	return 0
}
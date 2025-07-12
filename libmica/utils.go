package libmica

import (
	defs "mica-shim/definitions"
	"strings"
)

func startWithMicaPrefix(fieldName string) bool {
	return strings.HasPrefix(fieldName, defs.MicaAnnotationPrefix)
}

func isMicaAnnotation(fieldName string) string {
	return strings.TrimPrefix(fieldName, defs.MicaAnnotationPrefix)
}

// Test helper functions for accessing private state
func GetQueueSize() uint32 {
	if shmData == nil {
		return 0
	}
	shmData.lock()
	defer shmData.unlock()
	return shmData.count()
}

func IsQueueEmpty() bool {
	if shmData == nil {
		return true
	}
	shmData.lock()
	defer shmData.unlock()
	return shmData.isEmpty()
}

func GetMaxCPUs() uint32 {
	return maxCPUs
}

package libmica

import (
	"fmt"
	"mica-shim/cntr"
	defs "mica-shim/definitions"
	"os"
	"path/filepath"
	"strings"
)

func startWithMicaPrefix(fieldName string) bool {
	if strings.HasPrefix(fieldName, defs.MicaAnnotationPrefix) {
		return true
	} else {
		return false
	}
}

func isMicaAnnotation(fieldName string) string {
	return strings.TrimPrefix(fieldName, defs.MicaAnnotationPrefix)
}

func getFirmwarePath(bundle string, name string) (string, error) {
	// 0. check image LABEL "org.openeuler.mica.client.firmware = <relative path to firmware>" and search for it
	// if missing, 1. search bundle/.../<clientOSname>.elf 
	// if missing, 2.  log and search for binary in bundle recursively
	expected := cntr.ReadFirmwarePath(bundle, name)
	if expected != "" {
		if _, err := os.Stat(expected); err == nil {
			return expected, nil
		}
	}
	expected = filepath.Join(bundle, fmt.Sprintf("%s.elf", name))
	if _, err := os.Stat(expected); err == nil {
		return expected, nil
	}
	// recursively search for binary in bundle
	files, err := os.ReadDir(bundle)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if file.Name() == name {
			return filepath.Join(bundle, file.Name()), nil
		}
	}
	return "", fmt.Errorf("firmware not found in the whole bundle")
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

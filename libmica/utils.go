package libmica

import (
	"fmt"
	defs "mica-shim/definitions"
	"mica-shim/oci"
	"os"
	"path/filepath"
	"strings"
)

func startWithMicaPrefix(fieldName string) bool {
	return strings.HasPrefix(fieldName, defs.MicaAnnotationPrefix)
}

func isMicaAnnotation(fieldName string) string {
	return strings.TrimPrefix(fieldName, defs.MicaAnnotationPrefix)
}

func getFirmwarePath(bundle string) (string, error) {
	// 1. 尝试通过镜像annotation获取固件路径
	info, err := oci.ContainerInfoParse(bundle)
	if err != nil {
		return "", fmt.Errorf("failed to read firmware path: %w", err)
	}
	
	firmwarePath := info.FirmwarePath()
	if firmwarePath != "" {
		if _, err := os.Stat(firmwarePath); err == nil {
			return firmwarePath, nil
		}
	}
	
	files, err := os.ReadDir(bundle)
	if err != nil {
		return "", err
	}
	
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if strings.HasSuffix(file.Name(), ".elf") {
			return filepath.Join(bundle, file.Name()), nil
		}
	}
	
	rootfsPath := filepath.Join(bundle, "rootfs")
	if rootFiles, err := os.ReadDir(rootfsPath); err == nil {
		for _, file := range rootFiles {
			if file.IsDir() {
				continue
			}
			if strings.HasSuffix(file.Name(), ".elf") {
				return filepath.Join(rootfsPath, file.Name()), nil
			}
		}
	}
	
	return "", fmt.Errorf("firmware not found in the bundle")
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

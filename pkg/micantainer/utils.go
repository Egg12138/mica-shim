package micantainer

import (
	"context"
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/libmica"
	"os"
	"strconv"
	"strings"
	"time"
)

func createContainerInSandbox(sandbox SandboxTraits, config *ContainerConfig) (*RTOSTask, error) {
	// Create a new RTOS task with current time as start time
	task := &RTOSTask{
		StartTime:    time.Now(),
		TaskID:       config.ID,
		ReceiverAddr: 0x1000, // Dummy address for now
	}
	
	log.Infof("Created RTOS task %s for container %s", task.TaskID, config.ID)
	return task, nil
}

func startClient(ctx context.Context, sandbox SandboxTraits, c *Container) error {
	// TODO"
	conf, err := createMicaConf(c)
	if err != nil {
		return err
	}

	if err = libmica.Create(conf); err != nil {
		return err
	}
	
	// Start the RTOS client
	if err = libmica.Start(c.ID()); err != nil {
		return err
	}
	
	return nil
}

// 1. search bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TODO: Only copy values, the evaluation procedure is in the caller function
func createMicaConf(container *Container) (libmica.MicaClientConf, error) {
	config := container.config
	firmware := container.GetFirmwarePath()
	pedestal := HostPedType
	name := container.ID()
	cpu, err := container.GetClientCPU()
	conf := libmica.MicaClientConf{}
	if err != nil {
		return conf, fmt.Errorf("failed to get client cpu: %w", err)
	}
	mem := uint64(config.MemoryLimit)
	conf.InitWithOpts(libmica.MicaClientConfCreateOptions{
		CPU: []int{cpu},
		// TODO: dummy settings
		CPUCapacity: int(config.CpuQuota),
		CPUWeight:   int(config.CpuShares),
		Debug:       false,
		Memory:      int(mem),
		Name:        name,
		Network:     "",
		Path:        firmware,
		Ped:         pedestal.String(),
	})
	return conf, nil
}

// getSystemMemoryBytes returns the total system memory in bytes
// BUG:
func getSystemMemoryBytes() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		log.Warnf("failed to read /proc/meminfo, using default: %v", err)
		return 2 * 1024 * 1024 * 1024 // Default to 2GB
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if memKB, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					return memKB * 1024 // Convert KB to bytes
				}
			}
			break
		}
	}

	log.Warnf("failed to parse MemTotal from /proc/meminfo, using default")
	return 2 * 1024 * 1024 * 1024 // Default to 2GB
}

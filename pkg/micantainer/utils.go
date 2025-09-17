package micantainer

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/libmica"
	"mica-shim/pkg/pedestal"
	"mica-shim/pkg/utils"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/cpuset"
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
	conf, err := createMicaConf(c)
	if err != nil {
		return err
	}

	log.Infof("startClient: container=%s socket=%s mock=%t", c.ID(), defs.MicaCreatSocketPath, defs.IsMock)

	start := time.Now()
	if err = libmica.Create(conf); err != nil {
		log.Errorf("startClient: Create failed: %v", err)
		return err
	}
	log.Infof("startClient: Create OK in %s", time.Since(start))

	start = time.Now()
	if err = libmica.Start(c.ID()); err != nil {
		log.Errorf("startClient: Start failed: %v", err)
		return err
	}
	log.Infof("startClient: Start OK in %s", time.Since(start))

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

// if not pinning, vcpus coordinates with workload.
// Hence vcpu number for sandbox equal to sum of containers' ceil of CPU Cpucapacity
// 如果 milliCPUs = 0; 意味着所有的sandbox里的容器都没有 cpu quota限制，此时应该分配给sandbox 多少vcpus
// 是一个问题：
//  * 如果有一个容器设置了cpuset，对于该容器而言，调度器不会再允许它运行在所有CPU上了。
//  * 如果有多个容器都设置了cpuset，我们可以考虑它们的cpuset并集为一个 cpu pool, 整个sandbox的vcpu
// 只能运行在这个 cpu pool 中。目前这是一个仅在 MicRan 中保留的概念，未来我们会完成对pedestal 
// cpu pool 的兼容, 那么sandbox 为容器workload 申请的 vcpu number = Size(cpuSetUnion)
//  * 如果cpuset也完全没有设置，那么我们认为这是一个best effort sandbox
// 在算力上，应该设置capcapacity为=0,使pedestal不限制cpu用量
func calculateSandboxVCPUs(s *Sandbox) (uint32, error) { 
	milliCPUs := uint32(0)
	cpuSetCount := int(0)
	sandboxCpuset := cpuset.NewCPUSet()
	for _, cc  := range s.config.ContainerConfigs {
		if c, ok := s.containers[cc.ID]; ok && c.state.State == StateStopped {
			log.Debugf("skipped stopped container %s", c.ID())
			continue
		}

		if cpu := cc.Resources.CPU; cpu != nil {
			if cpu.Period != nil && cpu.Quota != nil {
				milliCPUs += utils.CalculateMilliCPUs(*cpu.Quota, *cpu.Period)
			}

			set , err := cpuset.Parse(cpu.Cpus)
			if err != nil {
				return 0, nil
			}

			sandboxCpuset = sandboxCpuset.Union(set)

		}
		cpuSetCount = sandboxCpuset.Size()

		// unconstrained cpu quota usage: limit cpu usage by size of cpuset, for example:
		if milliCPUs == 0 && cpuSetCount > 0 {
			return uint32(cpuSetCount), nil
		}

	}
	return utils.CalculateVCpusFromMilliCpus(milliCPUs), nil
}

func calculateSandboxMemory(s *Sandbox) uint64 { 
	memorySandbox := uint64(0)
	for _, cc := range s.config.ContainerConfigs {
		if c, ok := s.containers[cc.ID]; ok && c.state.State == StateStopped {
			log.Debugf("skipped stopped container %s", c.ID())
			continue
		}

		if m := cc.Resources.Memory; m != nil {
			currentLimit := int64(0)
			if m.Limit != nil && *m.Limit > 0 {
				currentLimit = *m.Limit
				memorySandbox += uint64(currentLimit)
				log.Debugf("sandbox memory limit + %d MiB",currentLimit)
			}

			if s.config.HugePageSupport {
				for _, lim := range cc.Resources.HugepageLimits {
					log.Debugf("sandbox hugepage limit + %d %s",lim.Limit, lim.Pagesize)
					memorySandbox += lim.Limit
				}
			}
		}
	}
	return memorySandbox
}


// getSystemMemoryBytes returns the total system memory in bytes
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


// TODO: not overrange of machine nrcpus
func CpusetRangeValid(cpuset cpuset.CPUSet) bool {
	return true
}



// Update resource for changed resource
// TODO: if modified => update 
func  updateContainerResource(c *Container, updated *pedestal.EssentialResource) error {
	old := c.me.ReadResource()
	if needUpdateCpuCap(*old.CpuCpacity, *updated.CpuCpacity) {

	}

	if needUpdateMemLimit(*old.MemoryLimitMB, *updated.MemoryLimitMB) {

	}

	if needUpdateVCpus(*old.Vcpu, *updated.Vcpu) {

	}

	if needUpdateCpuSet(old.ClientCpuSet, updated.ClientCpuSet) {

	}

	return nil 
}

func needUpdateCpuCap(old, updated uint32) bool {
	return true
}

func needUpdateMemLimit(old, updated uint32) bool {

	return true
}

func needUpdateVCpus(old, updated uint32) bool {

	return true
}

func needUpdateCpuSet(old, updated string) bool {

	return true
}
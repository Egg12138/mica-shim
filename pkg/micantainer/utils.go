package micantainer

import (
	"context"
	"fmt"
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

func initContainerTaskInSandbox(sandbox SandboxTraits, config *ContainerConfig) (*RTOSTask, error) {
	// Create a new RTOS task with current time as start time
	task := &RTOSTask{
		// start time should be the time of Start()
		CreateTime:    time.Time{},
		TaskID:       config.ID,
		ReservedAddr: 0x1000, // Dummy address for now
	}

	log.Infof("Created RTOS task %s for container %s", task.TaskID, config.ID)
	return task, nil
}

func startClient(ctx context.Context, sandbox SandboxTraits, c *Container) error {
	conf, err := createMicaClientConf(c)
	if err != nil {
		return err
	}

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
func createMicaClientConf(container *Container) (libmica.MicaClientConf, error) {
	config := container.config
	pedestal := HostPedType
	name := container.ID()
	cpus, err := container.GetClientCPU()
	conf := libmica.MicaClientConf{}
	if err != nil {
		return conf, fmt.Errorf("failed to get client cpu: %w", err)
	}
	cpuCap := int(config.CpuLimit)
	// Pre-calculate effective values for clarity.
	// Use VCPUNum prepared in ContainerConfig; it already reflects cpuset policy
	// or defaults to 1 when not specified.
	vcpus := int(config.VCPUNum)
	if vcpus <= 0 {
		vcpus = 1
	}
	// memoryMB (initial) comes from config.MemoryMinMB; clamp to max limit if set.
	maxMB := int(config.MemoryLimitMB)
	memMB := int(config.MemoryMinMB)
	if memMB <= 0 {
		memMB = 32
	}
	if maxMB > 0 && memMB > maxMB {
		memMB = maxMB
	}
	// MemoryLimitMB is already in MiB
	conf.InitWithOpts(libmica.MicaClientConfCreateOptions{
		CPU:         cpus,
		CPUCapacity: cpuCap,
		CPUWeight:   int(config.CpuShares),
		VCPUs:       vcpus,
		MemoryMB:    memMB,
		MaxMemMB:    maxMB,
		Name:        name,
		Path:        config.ElfPath,
		Ped:         pedestal.String(),
		PedCfg:      config.PedestalConf,
		Debug:       true,
	})
	return conf, nil
}

// Removed per-request: rely on ContainerConfig.VCPUNum as single source of truth.

// if not pinning, vcpus coordinates with workload.
// Hence vcpu number for sandbox equal to sum of containers' ceil of CPU Cpucapacity
// 如果 milliCPUs = 0; 意味着所有的sandbox里的容器都没有 cpu quota限制，此时应该分配给sandbox 多少vcpus
// 是一个问题：
//   - 如果有一个容器设置了cpuset，对于该容器而言，调度器不会再允许它运行在所有CPU上了。
//   - 如果有多个容器都设置了cpuset，我们可以考虑它们的cpuset并集为一个 cpu pool, 整个sandbox的vcpu
//
// 只能运行在这个 cpu pool 中。目前这是一个仅在 MicRan 中保留的概念，未来我们会完成对pedestal
// cpu pool 的兼容, 那么sandbox 为容器workload 申请的 vcpu number = Size(cpuSetUnion)
//   - 如果cpuset也完全没有设置，那么我们认为这是一个best effort sandbox
//
// 在算力上，应该设置capcapacity为=0,使pedestal不限制cpu用量
// calculateSandboxVCPUs returns the total VCPU count for the sandbox.
// Without a resource pool, this is a statistic that should reflect the sum
// of each container's configured vCPUs.
func calculateSandboxVCPUs(s *Sandbox) (uint32, error) {
	if s == nil || s.config == nil {
		return 0, fmt.Errorf("sandbox or sandbox config is nil")
	}

	total := uint32(0)
	for _, cc := range s.config.ContainerConfigs {
		if cc.Infra {
			continue
		}
		if c, ok := s.containers[cc.ID]; ok && c.state.State == StateStopped {
			log.Debugf("skipped stopped container %s", c.ID())
			continue
		}

		// Primary: use configured VCPUNum if set (already validated in ContainerConfig).
		if cc.VCPUNum > 0 {
			total += cc.VCPUNum
			continue
		}

		// Fallbacks for legacy/partial configs.
		if cpu := cc.Resources.CPU; cpu != nil {
			if cpu.Period != nil && cpu.Quota != nil && *cpu.Period != 0 {
				m := utils.CalculateMilliCPUs(*cpu.Quota, *cpu.Period)
				v := utils.CalculateVCpusFromMilliCpus(m)
				if v > 0 {
					total += v
					continue
				}
			}
			if cpu.Cpus != "" {
				set, err := cpuset.Parse(cpu.Cpus)
				if err == nil {
					total += uint32(set.Size())
					continue
				}
			}
		}

		// Last resort: count 1.
		total += 1
	}

	return total, nil
}

func calculateSandboxMemory(s *Sandbox) uint64 {
	memorySandbox := uint64(0)
	for _, cc := range s.config.ContainerConfigs {
		if cc.Infra {
			continue
		}
		if c, ok := s.containers[cc.ID]; ok && c.state.State == StateStopped {
			log.Debugf("skipped stopped container %s", c.ID())
			continue
		}

		if cc.Resources == nil {
			continue
		}

		if m := cc.Resources.Memory; m != nil {
			currentLimit := int64(0)
			if m.Limit != nil && *m.Limit > 0 {
				currentLimit = *m.Limit
				memorySandbox += uint64(currentLimit)
				log.Debugf("sandbox memory limit + %d MiB", currentLimit)
			}

			if s.config.HugePageSupport {
				for _, lim := range cc.Resources.HugepageLimits {
					log.Debugf("sandbox hugepage limit + %d %s", lim.Limit, lim.Pagesize)
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

func CpusetRangeValid(sortedCpuList []int) (bool, []int) {
	maxCpus := machineCPUNumber()
	outrange := []int{}

	for _, cpu := range sortedCpuList {
		// cpuid start from 0
		if cpu >= int(maxCpus) {
			outrange = append(outrange, cpu)
		}
	}

	if len(outrange) > 0 {
		log.Warnf("cpuset range is out of machine max cpu: %v", outrange)
		return false, outrange
	}

	return true, outrange
}

// Update resource for changed resource
func updateContainerResource(c *Container, updated *pedestal.EssentialResource) error {
	old := c.me.ReadResource()
	if needUpdateCpuCap(*old.CpuCpacity, *updated.CpuCpacity) {
		err := c.me.UpdateCPUCapacity(*updated.CpuCpacity)
		if err != nil {
			return fmt.Errorf("failed to update cpu capacity of %s: %v", c.id, err)
		}
		if *updated.CpuCpacity == 0 {
			log.Infof("container %s's cpu capacity is unlimited", c.id)
		}
	}

	if needUpdateMemLimit(*old.MemoryLimitMB, *updated.MemoryLimitMB) {
		err := c.me.UpdateMemoryLimit(*updated.MemoryLimitMB)
		if err != nil {
			return fmt.Errorf("failed to update max memory of %s: %v", c.id, err)
		}
	}

	if needUpdateCpuSet(old.ClientCpuSet, updated.ClientCpuSet) {
		err := c.me.UpdatePCPUConstrains(updated.ClientCpuSet)
		if err != nil {
			return fmt.Errorf("failed to update cpuset of vcpu: %v", err)
		}
	}

	if needUpdateCpuShare(*old.CPUWeight, *updated.CPUWeight) {
		err := c.me.UpdateCPUShare(*updated.CPUWeight)
		if err != nil {
			return fmt.Errorf("failed to set a different cpu weight for %s: %v", c.id, err)
		}
	}

	if needUpdateVCpus(*old.Vcpu, *updated.Vcpu) {
		old, newer, err := c.me.UpdateVCPUNum(*updated.Vcpu)
		if err != nil {
			log.Warnf("failed to update vcpu number: %v", err)
		}
		log.Infof("update vcpu number from %d to %d", old, newer)
	}

	return nil
}

func needUpdateCpuCap(old, updated uint32) bool {
	if old == updated {
		return false
	}
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

func needUpdateCpuShare(old, updated uint32) bool {

	return true
}

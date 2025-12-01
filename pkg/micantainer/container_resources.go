package micantainer

import (
	"github.com/opencontainers/runtime-spec/specs-go"
)

const miB = 1024 * 1024

func (cfg *ContainerConfig) ensureResources() *specs.LinuxResources {
	if cfg == nil {
		return nil
	}
	if cfg.Resources == nil {
		cfg.Resources = &specs.LinuxResources{}
	}
	return cfg.Resources
}

func (cfg *ContainerConfig) ensureCPU() *specs.LinuxCPU {
	res := cfg.ensureResources()
	if res == nil {
		return nil
	}
	if res.CPU == nil {
		res.CPU = &specs.LinuxCPU{}
	}
	return res.CPU
}

func (cfg *ContainerConfig) ensureMemory() *specs.LinuxMemory {
	res := cfg.ensureResources()
	if res == nil {
		return nil
	}
	if res.Memory == nil {
		res.Memory = &specs.LinuxMemory{}
	}
	return res.Memory
}

func (cfg *ContainerConfig) cpuSpec() *specs.LinuxCPU {
	if cfg == nil || cfg.Resources == nil {
		return nil
	}
	return cfg.Resources.CPU
}

func (cfg *ContainerConfig) memorySpec() *specs.LinuxMemory {
	if cfg == nil || cfg.Resources == nil {
		return nil
	}
	return cfg.Resources.Memory
}

func (cfg *ContainerConfig) cpuCapacity() uint32 {
	cpu := cfg.cpuSpec()
	if cpu == nil || cpu.Quota == nil || cpu.Period == nil || *cpu.Period == 0 {
		return 0
	}
	if *cpu.Quota <= 0 {
		return 0
	}
	capacity := *cpu.Quota / int64(*cpu.Period)
	if capacity <= 0 {
		return 0
	}
	return uint32(capacity * num2CapRatio)
}

func (cfg *ContainerConfig) cpuShares() uint64 {
	cpu := cfg.cpuSpec()
	if cpu == nil || cpu.Shares == nil {
		return 0
	}
	return *cpu.Shares
}

func (cfg *ContainerConfig) cpuMask() string {
	cpu := cfg.cpuSpec()
	if cpu == nil {
		return ""
	}
	return cpu.Cpus
}

func (cfg *ContainerConfig) memoryLimitMB() uint32 {
	return bytesToMiB(cfg.memoryLimitBytes())
}

func (cfg *ContainerConfig) memoryReservationMB() uint32 {
	return bytesToMiB(cfg.memoryReservationBytes())
}

func (cfg *ContainerConfig) memoryLimitBytes() *int64 {
	mem := cfg.memorySpec()
	if mem == nil {
		return nil
	}
	return mem.Limit
}

func (cfg *ContainerConfig) memoryReservationBytes() *int64 {
	mem := cfg.memorySpec()
	if mem == nil {
		return nil
	}
	return mem.Reservation
}

func (cfg *ContainerConfig) setMemoryReservationMB(mb uint32) {
	mem := cfg.ensureMemory()
	if mem == nil {
		return
	}
	mem.Reservation = miBToBytes(mb)
}

// CPUCapacity reports the configured CPU capacity in units of 0.01 CPUs.
func (cfg *ContainerConfig) CPUCapacity() uint32 {
	return cfg.cpuCapacity()
}

// CPUShares reports the configured CPU shares weight.
func (cfg *ContainerConfig) CPUShares() uint64 {
	return cfg.cpuShares()
}

// CPUSet returns the configured CPU affinity mask.
func (cfg *ContainerConfig) CPUSet() string {
	return cfg.cpuMask()
}

// MemoryLimitMiB returns the configured memory limit in MiB.
func (cfg *ContainerConfig) MemoryLimitMiB() uint32 {
	return cfg.memoryLimitMB()
}

// MemoryReservationMiB returns the configured memory reservation in MiB.
func (cfg *ContainerConfig) MemoryReservationMiB() uint32 {
	return cfg.memoryReservationMB()
}

// SetMemoryReservationMB records the requested memory reservation.
func (cfg *ContainerConfig) SetMemoryReservationMB(mb uint32) {
	cfg.setMemoryReservationMB(mb)
}

func bytesToMiB(value *int64) uint32 {
	if value == nil || *value <= 0 {
		return 0
	}
	return uint32(*value / miB)
}

func miBToBytes(value uint32) *int64 {
	v := int64(value) * miB
	return &v
}

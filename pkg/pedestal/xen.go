// pakcage pedestal currently is basically a XEN package!
// TODO: re-orgnize the package for better construction
package pedestal

import (
	"bufio"
	"bytes"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/cpuset"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const DefaultCgroupShare = 1024
const DefaultXenWeight = 256
const ShareWeightRatio = DefaultCgroupShare / DefaultXenWeight

// xl info:
// `host                   : qemu-aarch64
// release                : 5.10.0-openeuler
// version                : #1 SMP PREEMPT Sat Jun 7 07:26:44 UTC 2025
// machine                : aarch64
// nr_cpus                : 3
// max_cpu_id             : 2
// nr_nodes               : 1
// cores_per_socket       : 1
// threads_per_core       : 1
// cpu_mhz                : 62.500
// hw_caps                : 00000000:00000000:00000000:00000000:00000000:00000000:00000000:00000000
// virt_caps              : hvm hap vpmu gnttab-v1
// arm_sve_vector_length  : 0
// total_memory           : 2048
// free_memory            : 1427
// sharing_freed_memory   : 0
// sharing_used_memory    : 0
// outstanding_claims     : 0
// free_cpus              : 0
// xen_major              : 4
// xen_minor              : 18
// xen_extra              : .2
// xen_version            : 4.18.2
// xen_caps               : xen-3.0-aarch64 xen-3.0-armv7l
// xen_scheduler          : credit2
// xen_pagesize           : 4096
// platform_params        : virt_start=0x0
// xen_changeset          :
// xen_commandline        : console=dtuart dtuart=/pl011Git commit '9000000' (see below for commit info) dom0_mem=512M
// cc_compiler            : aarch64-openeuler-linux-gnu-gcc (crosstool-NG 1.26.0) 12.3.1 20
// cc_compile_by          :
// cc_compile_domain      :
// cc_compile_date        : 2025-06-07
// build_id               : d54faddad0e57e72305a485d9b89288188c56ae8
// xend_config_format     : 4`


type XlInfo struct {
	host        string
	machine     string
	// max physical cpus that Xen can handle
	nrCpus      uint32
	totalMemory uint64
	freeMemory  uint64
	xlver string
	
	maxCpuId         uint32  
	// Cores per socket (NUMA/topology awareness)
	coresPerSocket   uint32  
	// Threads per core (SMT/hyperthreading info)
	threadsPerCore   uint32  
	cpuMhz          float64  
	// number of cpus that are not allocated in **a cpu pool**
	freeCpus        uint32   
	
	xenCaps         string   
	// Scheduler type (credit, credit2, etc.)
	// decides in Xen building, default to be credit2 for now
	xenScheduler    string   
	xenPagesize     uint32   
	virtCaps        string   

	// Memory claims pending (affects available memory calculations)
	outstandingClaims uint64 
	// Shared memory freed (memory reuse optimization)
	sharingFreedMemory uint64 
	// Shared memory used (current shared memory usage)
	sharingUsedMemory  uint64 
	
	platformParams  string   
	// Xen boot parameters 
	xenCommandline  string   
	
	// ARM-specific fields (for aarch64 systems - architecture optimizations)
	// turn off by default
	armSVEVectorLength uint32
}

type XlVcpuInfo struct {
}

type xlSubCmd string

const (
	info   xlSubCmd = "info"
	vcpulist  xlSubCmd = "vcpu-list"
	vcpupin  xlSubCmd = "vcpu-pin"
	vmlist   xlSubCmd = "vm-list"
	pause    xlSubCmd = "pause"
	resume   xlSubCmd = "unpause"
)

func newxl(subcmd xlSubCmd, args ...string) *exec.Cmd {
	cmdArgs := []string{string(subcmd)}
	cmdArgs = append(cmdArgs, args...)
	return exec.Command("xl", cmdArgs...)
}


func xlvcpu() (*XlVcpuInfo, error) {
	var cmd *exec.Cmd
	var out bytes.Buffer
	cmd = newxl(vcpulist)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run xl info: %v", err)
	}

	return parseXlVcpuInfo(out.String())
}

func xinfo() (*XlInfo, error) {
	cmd := newxl(info)

	// Capture output
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run xl info: %v", err)
	}

	// Parse the output
	return parseXlInfo(out.String())
}


func parseXlInfo(output string) (*XlInfo, error) {
	info := &XlInfo{}
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Split on ":" and trim whitespace
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "host":
			info.host = value
		case "machine":
			info.machine = value
		case "nr_cpus":
			if nrCpus, err := strconv.ParseUint(value, 10, 32); err == nil {
				info.nrCpus = uint32(nrCpus)
			}
		case "total_memory":
			if totalMemory, err := strconv.ParseUint(value, 10, 64); err == nil {
				info.totalMemory = totalMemory
			}
		case "free_memory":
			if freeMemory, err := strconv.ParseUint(value, 10, 64); err == nil {
				info.freeMemory = freeMemory
			}
		case "xen_major":
			// Build xl version
			info.xlver = value
		case "xen_minor":
			if info.xlver != "" {
				info.xlver += "." + value
			}
		case "xen_extra":
			if info.xlver != "" {
				info.xlver += value
			}
			
		// CPU topology and scheduling information
		case "max_cpu_id":
			if maxCpuId, err := strconv.ParseUint(value, 10, 32); err == nil {
				info.maxCpuId = uint32(maxCpuId)
			}
		case "cores_per_socket":
			if coresPerSocket, err := strconv.ParseUint(value, 10, 32); err == nil {
				info.coresPerSocket = uint32(coresPerSocket)
			}
		case "threads_per_core":
			if threadsPerCore, err := strconv.ParseUint(value, 10, 32); err == nil {
				info.threadsPerCore = uint32(threadsPerCore)
			}
		case "cpu_mhz":
			if cpuMhz, err := strconv.ParseFloat(value, 64); err == nil {
				info.cpuMhz = cpuMhz
			}

		case "free_cpus":
			if freeCpus, err := strconv.ParseUint(value, 10, 32); err == nil {
				info.freeCpus = uint32(freeCpus)
			}
			
		// Xen capabilities and features
		case "xen_caps":
			info.xenCaps = value
		case "xen_scheduler":
			info.xenScheduler = value
		case "xen_pagesize":
			if xenPagesize, err := strconv.ParseUint(value, 10, 32); err == nil {
				info.xenPagesize = uint32(xenPagesize)
			}
		case "virt_caps":
			info.virtCaps = value
			
		// Memory management details
		case "outstanding_claims":
			if outstandingClaims, err := strconv.ParseUint(value, 10, 64); err == nil {
				info.outstandingClaims = outstandingClaims
			}
		case "sharing_freed_memory":
			if sharingFreedMemory, err := strconv.ParseUint(value, 10, 64); err == nil {
				info.sharingFreedMemory = sharingFreedMemory
			}
		case "sharing_used_memory":
			if sharingUsedMemory, err := strconv.ParseUint(value, 10, 64); err == nil {
				info.sharingUsedMemory = sharingUsedMemory
			}
			
		// Platform-specific information
		case "platform_params":
			info.platformParams = value
		case "xen_commandline":
			info.xenCommandline = value
			
		// ARM-specific fields
		case "arm_sve_vector_length":
			if armSVEVectorLength, err := strconv.ParseUint(value, 10, 32); err == nil {
				info.armSVEVectorLength = uint32(armSVEVectorLength)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading xl info output: %v", err)
	}

	return info, nil
}

func parseXlVcpuInfo(output string) (*XlVcpuInfo, error) {
	return nil, nil
}

func (xi *XlInfo) nodePhysicalCPUNum() uint32 {
	return xi.nrCpus
}

func MaxCPUNum() uint32 {
	if defs.IsMock {
		return uint32(runtime.NumCPU())
	}
	i, err := xinfo()
	if err != nil {
		return uint32(runtime.NumCPU())
	}
	return i.nodePhysicalCPUNum()
}

// For cases, id is truncated id
func Resume(id string) error {
	if defs.IsMock {
		return nil
	}
	cmd := newxl(resume, id)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to resume %s: %v", id, err)
	}
	log.Debugf("resume %s successfully", id)
	return nil
}

func Pause(id string) error {
	if defs.IsMock {
		return nil
	}
	cmd := newxl(pause, id)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to pause %s: %v", id, err)
	}
	log.Debugf("pause %s successfully", id)
	return nil
}


func XenDefaultPedConf() string {
	return "image.bin"
}


func LinuxResource2Essential(spec *specs.Spec) *EssentialResource {
	r := &EssentialResource{}	
	// cpu
	cpu := spec.Linux.Resources.CPU
	if cpu.Quota != nil && cpu.Period != nil && *cpu.Period > 0 {
		r.CpuPeriod = *cpu.Period
		r.CpuQuota = *cpu.Quota
		cpuCapacity := *cpu.Quota / int64(*cpu.Period)
		if cpuCapacity > 0 {
			r.CpuCpacity = uint32(100 * cpuCapacity)
		} else {
			r.CpuCpacity = 0
		}
	} else {
		log.Debugf("cpu quota/period pair = < %s:%s > is incomplete,Xen scheduler will allow all possible cpu to container", cpu.Quota, cpu.Period)
		r.CpuCpacity = 0
	}

	if cpu.Shares != nil {
		calculatedWeight := *cpu.Shares / ShareWeightRatio
		if calculatedWeight < 1 {
			r.CPUWeight = 1
		} else if calculatedWeight > 65535 {
			log.Debugf("cpu.Shares %d is too high, resulting weight is greater than 65535. Clamping to 65535.", *cpu.Shares)
			r.CPUWeight = 65535
		} else {
			r.CPUWeight = uint32(calculatedWeight)
		}
	} else {
		log.Debugf("cpu shares is nil, use default weight %d", DefaultXenWeight)
		r.CPUWeight = DefaultXenWeight
	}

	cpus, set, vcpuNum := validateCPUSet(cpu.Cpus)

	log.Debugf("pinning cpu set = %v, parse to %v", cpus, set)
	// vcpuNum = calculateVCPU(&set, int(r.CpuCpacity))
	r.Vcpu = uint32(vcpuNum)

	// mem
	mem := spec.Linux.Resources.Memory
	if mem != nil && mem.Limit != nil {
		r.MemoryLimit = uint32(*mem.Limit / 1024 / 1024)
	}


	// net

	return r
}

// assume cpu set is valid
// do hard affinity only
func PinVCPU(shortId, cpus string) error {
	if defs.IsMock {
		return nil
	}
	cmd := newxl(vcpupin, shortId, "all", cpus)
	log.Debugf("run %s to pinning vcpu %s to %s", cmd.String(), cpus, shortId)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to pause %s: %v", shortId, err)
	}
	return nil
}

// the format of two cpuset are the same, but micran needs to calculate host cpu resource and so on
// TODO: 	valid Cpu set
func validateCPUSet(s string) (validSet string, set cpuset.CPUSet, vcpus uint32) {
	set, err := cpuset.Parse(s)
	if err != nil {
		return "", set, 0
	}
	validSet = ""
	return validSet, set, uint32(set.Size())
}

// if cpuSet is empty, container will see cpu the same as maxcpu??
// TODO: not sure the default value
func calculateVCPU(cpuSet *cpuset.CPUSet, vcpuAssigned int) int {
	if vcpuAssigned == 0 {
		vcpuAssigned = 1
	}
	if cpuSet == nil {
		return vcpuAssigned // or 1?
		// return 1
	}
	return cpuSet.Size()
}

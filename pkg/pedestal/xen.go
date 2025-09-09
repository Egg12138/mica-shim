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
//             host                   : scarlett
//             release                : 3.1.0-rc4+
//             version                : #1001 SMP Wed Oct 19 11:09:54 UTC 2011
//             machine                : x86_64
//             nr_cpus                : 4
//             nr_nodes               : 1
//             cores_per_socket       : 4
//             threads_per_core       : 1
//             cpu_mhz                : 2266
//             hw_caps                : bfebfbff:28100800:00000000:00003b40:009ce3bd:00000000:00000
// 001:00000000
//             virt_caps              : hvm hvm_directio
//             total_memory           : 6141
//             free_memory            : 4274
//             free_cpus              : 0
//             outstanding_claims     : 0
//             xen_major              : 4
//             xen_minor              : 2
//             xen_extra              : -unstable
//             xen_caps               : xen-3.0-x86_64 xen-3.0-x86_32p hvm-3.0-x86_32 hvm-3.0-x86_3
// 2p hvm-3.0-x86_64
//             xen_scheduler          : credit
//             xen_pagesize           : 4096
//             platform_params        : virt_start=0xffff800000000000
//             xen_changeset          : Wed Nov 02 17:09:09 2011 +0000 24066:54a5e994a241
//             xen_commandline        : com1=115200,8n1 guest_loglvl=all dom0_mem=750M console=com1
//             cc_compiler            : gcc version 4.4.5 (Debian 4.4.5-8)
//             cc_compile_by          : sstabellini
//             cc_compile_domain      : uk.xensource.com
//             cc_compile_date        : Tue Nov  8 12:03:05 UTC 2011
//             xend_config_format     : 4

type XlInfo struct {
	host        string
	machine     string
	nrCpus      uint32
	totalMemory uint64
	freeMemory  uint64
	// xlver = <info::xen_major>.<info::xen_minor>.<info::xen_extra>
	xlver string
	// TODO: attach Xl... struct into XlInfo, in order to parse once, reuse thoudsand times
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

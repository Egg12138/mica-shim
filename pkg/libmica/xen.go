package libmica

import (
	"bufio"
	"bytes"
	"fmt"
	defs "mica-shim/definitions"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

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
	info     xlSubCmd = "info"
	vcpulist xlSubCmd = "vcpu-list"
	vcpupin  xlSubCmd = "vcpu-pin"
	vmlist   xlSubCmd = "vm-list"
)

func newCommand(subcmd xlSubCmd) *exec.Cmd {
	return exec.Command("xl", string(subcmd))
}

func xlvcpu() (*XlVcpuInfo, error) {
	var cmd *exec.Cmd
	var out bytes.Buffer
	cmd = newCommand(vcpulist)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run xl info: %v", err)
	}

	return parseXlVcpuInfo(out.String())
}

func xinfo() (*XlInfo, error) {
	cmd := newCommand(info)

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

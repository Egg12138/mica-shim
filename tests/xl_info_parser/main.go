package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// XlInfo struct matches the one from xen.go
type XlInfo struct {
	host    string
	machine string
	// max physical cpus that Xen can handle
	nrCpus      uint32
	totalMemory uint64
	freeMemory  uint64
	// xlver = <info::xen_major>.<info::xen_minor>.<info::xen_extra>
	xlver string

	// CPU topology and scheduling information
	maxCpuId       uint32  // Maximum CPU ID (useful for CPU topology awareness)
	coresPerSocket uint32  // Cores per socket (NUMA/topology awareness)
	threadsPerCore uint32  // Threads per core (SMT/hyperthreading info)
	cpuMhz         float64 // CPU frequency (performance calculations and RTOS timing)
	freeCpus       uint32  // Available CPUs for allocation (resource management)

	// Xen capabilities and features (critical for feature detection)
	xenCaps      string // Xen capabilities (xen-3.0-aarch64, etc.)
	xenScheduler string // Scheduler type (credit, credit2, etc.) - affects RTOS scheduling
	xenPagesize  uint32 // Page size (memory management and allocation alignment)
	virtCaps     string // Virtualization capabilities (hvm, hap, etc.)

	// Memory management details (for resource allocation decisions)
	outstandingClaims  uint64 // Memory claims pending (affects available memory calculations)
	sharingFreedMemory uint64 // Shared memory freed (memory reuse optimization)
	sharingUsedMemory  uint64 // Shared memory used (current shared memory usage)

	// Platform-specific information (for hardware-specific optimizations)
	platformParams string // Platform-specific parameters (hardware-specific settings)
	xenCommandline string // Xen boot parameters (dom0_mem, etc. - affects resource availability)

	// ARM-specific fields (for aarch64 systems - architecture optimizations)
	armSVEVectorLength uint32 // SVE vector length for ARM optimizations (RTOS performance)
}

// Sample xl info output for testing (real aarch64 Xen output)
const sampleXlInfoOutput = `host                   : qemu-aarch64
release                : 5.10.0-openeuler
version                : #1 SMP PREEMPT Sat Jun 7 07:26:44 UTC 2025
machine                : aarch64
nr_cpus                : 3
max_cpu_id             : 2
nr_nodes               : 1
cores_per_socket       : 1
threads_per_core       : 1
cpu_mhz                : 62.500
hw_caps                : 00000000:00000000:00000000:00000000:00000000:00000000:00000000:00000000
virt_caps              : hvm hap vpmu gnttab-v1
arm_sve_vector_length  : 0
total_memory           : 2048
free_memory            : 1427
sharing_freed_memory   : 0
sharing_used_memory    : 0
outstanding_claims     : 0
free_cpus              : 0
xen_major              : 4
xen_minor              : 18
xen_extra              : .2
xen_version            : 4.18.2
xen_caps               : xen-3.0-aarch64 xen-3.0-armv7l
xen_scheduler          : credit2
xen_pagesize           : 4096
platform_params        : virt_start=0x0
xen_changeset          :
xen_commandline        : console=dtuart dtuart=/pl011Git commit '9000000' (see below for commit info) dom0_mem=512M
cc_compiler            : aarch64-openeuler-linux-gnu-gcc (crosstool-NG 1.26.0) 12.3.1 20
cc_compile_by          :
cc_compile_domain      :
cc_compile_date        : 2025-06-07
build_id               : d54faddad0e57e72305a485d9b89288188c56ae8
xend_config_format     : 4`

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

// runXlInfo runs the actual xl info command
func runXlInfo() (*XlInfo, error) {
	cmd := exec.Command("xl", "info")

	// Capture output
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run xl info: %v", err)
	}

	return parseXlInfo(out.String())
}

func printXlInfo(info *XlInfo, title string) {
	fmt.Printf("\n=== %s ===\n", title)
	fmt.Printf("Host: %s\n", info.host)
	fmt.Printf("Machine: %s\n", info.machine)
	fmt.Printf("Number of CPUs: %d\n", info.nrCpus)
	fmt.Printf("Total Memory: %d MB\n", info.totalMemory)
	fmt.Printf("Free Memory: %d MB\n", info.freeMemory)
	fmt.Printf("Xen Version: %s\n", info.xlver)

	// CPU topology and scheduling information
	if info.maxCpuId > 0 {
		fmt.Printf("Max CPU ID: %d\n", info.maxCpuId)
	}
	if info.coresPerSocket > 0 {
		fmt.Printf("Cores per Socket: %d\n", info.coresPerSocket)
	}
	if info.threadsPerCore > 0 {
		fmt.Printf("Threads per Core: %d\n", info.threadsPerCore)
	}
	if info.cpuMhz > 0 {
		fmt.Printf("CPU Frequency: %.1f MHz\n", info.cpuMhz)
	}
	if info.freeCpus > 0 {
		fmt.Printf("Free CPUs: %d\n", info.freeCpus)
	}

	// Xen capabilities and features
	if info.xenCaps != "" {
		fmt.Printf("Xen Capabilities: %s\n", info.xenCaps)
	}
	if info.xenScheduler != "" {
		fmt.Printf("Xen Scheduler: %s\n", info.xenScheduler)
	}
	if info.xenPagesize > 0 {
		fmt.Printf("Xen Page Size: %d bytes\n", info.xenPagesize)
	}
	if info.virtCaps != "" {
		fmt.Printf("Virtualization Capabilities: %s\n", info.virtCaps)
	}

	// Memory management details
	if info.outstandingClaims > 0 {
		fmt.Printf("Outstanding Claims: %d MB\n", info.outstandingClaims)
	}
	if info.sharingFreedMemory > 0 {
		fmt.Printf("Sharing Freed Memory: %d MB\n", info.sharingFreedMemory)
	}
	if info.sharingUsedMemory > 0 {
		fmt.Printf("Sharing Used Memory: %d MB\n", info.sharingUsedMemory)
	}

	// Platform-specific information
	if info.platformParams != "" {
		fmt.Printf("Platform Parameters: %s\n", info.platformParams)
	}
	if info.xenCommandline != "" {
		fmt.Printf("Xen Command Line: %s\n", info.xenCommandline)
	}

	// ARM-specific fields
	if info.armSVEVectorLength > 0 {
		fmt.Printf("ARM SVE Vector Length: %d\n", info.armSVEVectorLength)
	}
}

func main() {
	useSample := flag.Bool("sample", false, "Use sample data instead of running xl info")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	flag.Parse()

	if *verbose {
		fmt.Println("XL Info Parser Test")
		fmt.Println("==================")
	}

	var info *XlInfo
	var err error

	if *useSample {
		if *verbose {
			fmt.Println("Using sample xl info data...")
		}
		info, err = parseXlInfo(sampleXlInfoOutput)
		if err != nil {
			log.Fatalf("Failed to parse sample xl info: %v", err)
		}
		printXlInfo(info, "Sample Data Results")
	} else {
		if *verbose {
			fmt.Println("Running 'xl info' command...")
		}

		_, err = exec.LookPath("xl")
		if err != nil {
			fmt.Println("xl command not found. Using sample data instead.")
			fmt.Println("To test with real xl info, run this on a Xen system with xl installed.")
			info, err = parseXlInfo(sampleXlInfoOutput)
			if err != nil {
				log.Fatalf("Failed to parse sample xl info: %v", err)
			}
			printXlInfo(info, "Sample Data Results (xl not available)")
		} else {
			info, err = runXlInfo()
			if err != nil {
				log.Fatalf("Failed to run xl info: %v", err)
			}
			printXlInfo(info, "Real XL Info Results")
		}
	}

	if *verbose {
		fmt.Println("\n=== Additional Tests ===")
		fmt.Printf("CPU count > 0: %t\n", info.nrCpus > 0)
		fmt.Printf("Total memory > 0: %t\n", info.totalMemory > 0)
		fmt.Printf("Free memory <= Total memory: %t\n", info.freeMemory <= info.totalMemory)

		if info.totalMemory > 0 {
			freePercentage := float64(info.freeMemory) / float64(info.totalMemory) * 100
			fmt.Printf("Free memory percentage: %.1f%%\n", freePercentage)
		}
	}

	fmt.Println("\nTest completed successfully!")
}

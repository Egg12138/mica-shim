package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)


type VCPUEntry struct {
	DomainName   string
	DomainID     int
	VCPUID       int
	CPU          int  // -1 if offline
	State        string
	TimeSeconds  float64
	HardAffinity string
	SoftAffinity string
}

// XlVcpuInfo contains parsed VCPU information grouped by domain
type XlVcpuInfo struct {
	DomainVCPUMap map[string][]VCPUEntry
}

func main() {
	sampleFlag := flag.Bool("sample", false, "Use sample data from vcpu-list-example.txt")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose output")
	flag.Parse()

	var output string
	var err error

	if *sampleFlag {
		// Read from sample file
		output, err = readSampleData()
		if err != nil {
			fmt.Printf("Error reading sample data: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Run actual xl vcpu-list command
		output, err = runXlVcpuList()
		if err != nil {
			fmt.Printf("Error running xl vcpu-list command: %v\n", err)
			fmt.Println("Falling back to sample data...")
			output, err = readSampleData()
			if err != nil {
				fmt.Printf("Error reading sample data: %v\n", err)
				os.Exit(1)
			}
		}
	}

	vcpuInfo, err := parseXlVcpuInfo(output)
	if err != nil {
		fmt.Printf("Error parsing vcpu-list output: %v\n", err)
		os.Exit(1)
	}

	if *verboseFlag {
		printVerboseOutput(vcpuInfo)
	} else {
		printSummary(vcpuInfo)
	}
}

func runXlVcpuList() (string, error) {
	cmd := exec.Command("xl", "vcpu-list")
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("xl vcpu-list failed: %v, stderr: %s", err, stderr.String())
	}
	
	return stdout.String(), nil
}

func readSampleData() (string, error) {
	data, err := os.ReadFile("vcpu-list-example.txt")
	if err != nil {
		return "", fmt.Errorf("failed to read sample file: %v", err)
	}
	return string(data), nil
}

func parseXlVcpuInfo(output string) (*XlVcpuInfo, error) {
	info := &XlVcpuInfo{
		DomainVCPUMap: make(map[string][]VCPUEntry),
	}

	scanner := bufio.NewScanner(strings.NewReader(output))

	// Skip to the header
	headerFound := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "Name") && strings.Contains(line, "Affinity") {
			headerFound = true
			break
		}
	}

	if !headerFound {
		return nil, fmt.Errorf("could not find vcpu-list header")
	}

	// Parse data lines
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		vcpu, err := parseVcpuLine(line)
		if err != nil {
			return nil, fmt.Errorf("error parsing line '%s': %v", line, err)
		}

		// Group VCPUs by domain name
		info.DomainVCPUMap[vcpu.DomainName] = append(info.DomainVCPUMap[vcpu.DomainName], vcpu)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading output: %v", err)
	}

	return info, nil
}

func parseVcpuLine(line string) (VCPUEntry, error) {
	// Use regex to parse the fixed-width format
	// Format: Name(32) ID(5) VCPU(5) CPU(5) State(5) Time(9) Affinity(variable)
	// State field can have various formats like "r--", "-b-", "--p", etc.
	re := regexp.MustCompile(`^(\S.{0,31})\s+(\d+)\s+(\d+)\s+(\d+|-)\s+([a-z-]{3})\s+([\d.]+)\s+(.+)$`)
	matches := re.FindStringSubmatch(line)
	if matches == nil || len(matches) < 8 {
		return VCPUEntry{}, fmt.Errorf("line format does not match: %s", line)
	}

	domainName := strings.TrimSpace(matches[1])
	domainID, _ := strconv.Atoi(matches[2])
	vcpuid, _ := strconv.Atoi(matches[3])

	cpu := -1
	if matches[4] != "-" {
		cpu, _ = strconv.Atoi(matches[4])
	}

	// State is already a complete 3-character string
	state := matches[5]

	timeSeconds, _ := strconv.ParseFloat(matches[6], 64)

	// Parse affinity
	affinity := matches[7]
	parts := strings.Split(affinity, " / ")
	hardAffinity := parts[0]
	softAffinity := ""
	if len(parts) > 1 {
		softAffinity = parts[1]
	}

	return VCPUEntry{
		DomainName:   domainName,
		DomainID:     domainID,
		VCPUID:       vcpuid,
		CPU:          cpu,
		State:        state,
		TimeSeconds:  timeSeconds,
		HardAffinity: hardAffinity,
		SoftAffinity: softAffinity,
	}, nil
}

func printVerboseOutput(info *XlVcpuInfo) {
	fmt.Println("=== XL VCPU-LIST PARSER VERBOSE OUTPUT ===")
	fmt.Printf("Found %d domains\n", len(info.DomainVCPUMap))

	for domainName, vcpus := range info.DomainVCPUMap {
		fmt.Printf("\nDomain: %s (%d VCPUs)\n", domainName, len(vcpus))
		fmt.Println("----------------------------------------")
		for _, vcpu := range vcpus {
			cpuStatus := "offline"
			if vcpu.CPU != -1 {
				cpuStatus = fmt.Sprintf("CPU %d", vcpu.CPU)
			}

			fmt.Printf("  VCPU %d: %s, State: %s, Time: %.1fs\n", 
				vcpu.VCPUID, cpuStatus, vcpu.State, vcpu.TimeSeconds)
			fmt.Printf("    Hard Affinity: %s\n", vcpu.HardAffinity)
			fmt.Printf("    Soft Affinity: %s\n", vcpu.SoftAffinity)
		}
	}
}

func printSummary(info *XlVcpuInfo) {
	fmt.Println("=== XL VCPU-LIST PARSER SUMMARY ===")
	fmt.Printf("Total domains: %d\n", len(info.DomainVCPUMap))

	totalVCPUs := 0
	onlineVCPUs := 0
	for _, vcpus := range info.DomainVCPUMap {
		totalVCPUs += len(vcpus)
		for _, vcpu := range vcpus {
			if vcpu.CPU != -1 {
				onlineVCPUs++
			}
		}
	}

	fmt.Printf("Total VCPUs: %d\n", totalVCPUs)
	fmt.Printf("Online VCPUs: %d\n", onlineVCPUs)
	fmt.Printf("Offline VCPUs: %d\n", totalVCPUs-onlineVCPUs)

	fmt.Println("\nDomains and their VCPU counts:")
	for domainName, vcpus := range info.DomainVCPUMap {
		fmt.Printf("  %s: %d VCPUs\n", domainName, len(vcpus))
	}
}

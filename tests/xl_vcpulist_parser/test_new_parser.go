package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// VCPUEntry represents a parsed VCPU entry
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

// Original parser function (from xen.go)
func parseVcpuLineOld(line string) (VCPUEntry, error) {
	// line format: Name(32) ID(5) VCPU(5) CPU(5) State(5) Time(9) Affinity(variable)
	re := regexp.MustCompile(`^(\S.{31})\s+(\d+)\s+(\d+)\s+(\d+|-)\s+([r-])([b-])([-])\s+([\d.]+)\s+(.+)$`)
	matches := re.FindStringSubmatch(line)
	if matches == nil {
		return VCPUEntry{}, fmt.Errorf("regex match failed")
	}

	domainName := strings.TrimSpace(matches[1])
	domainID, _ := strconv.Atoi(matches[2])
	vcpuid, _ := strconv.Atoi(matches[3])

	cpu := -1
	if matches[4] != "-" {
		cpu, _ = strconv.Atoi(matches[4])
	}

	state := "---"
	if matches[4] != "-" {
		state = matches[5] + matches[6] + matches[7]
	}

	timeSeconds, _ := strconv.ParseFloat(matches[8], 64)

	affinity := matches[9]
	parts := strings.Split(affinity, " / ")
	hardAffinity := parts[0]
	softAffinity := ""
	if len(parts) > 1 {
		softAffinity = parts[1]
	}

	return VCPUEntry{
		DomainName:    domainName,
		DomainID:     domainID,
		VCPUID:       vcpuid,
		CPU:          cpu,
		State:        state,
		TimeSeconds:  timeSeconds,
		HardAffinity: hardAffinity,
		SoftAffinity: softAffinity,
	}, nil
}

// New parser function (more robust)
func parseVcpuLineNew(line string) (VCPUEntry, error) {
	// Skip header line
	if strings.Contains(line, "Name") && strings.Contains(line, "ID") {
		return VCPUEntry{}, fmt.Errorf("header line skipped")
	}

	// Split by whitespace but handle domain names with spaces
	fields := strings.Fields(line)
	if len(fields) < 8 {
		return VCPUEntry{}, fmt.Errorf("insufficient fields: got %d, need at least 8", len(fields))
	}

	// Find the affinity field (contains " / ")
	affinityIndex := -1
	for i, field := range fields {
		if strings.Contains(field, "/") {
			affinityIndex = i
			break
		}
	}
	if affinityIndex == -1 || affinityIndex < 7 {
		return VCPUEntry{}, fmt.Errorf("affinity field not found or in wrong position")
	}

	// Domain name is everything before the ID field (which is a number)
	var domainName string
	var domainNameParts []string
	for i := 0; i < affinityIndex-6; i++ {
		domainNameParts = append(domainNameParts, fields[i])
	}
	domainName = strings.Join(domainNameParts, " ")

	// Parse the numeric fields
	offset := affinityIndex - 6
	domainID, err := strconv.Atoi(fields[offset])
	if err != nil {
		return VCPUEntry{}, fmt.Errorf("failed to parse domain ID: %v", err)
	}

	vcpuid, err := strconv.Atoi(fields[offset+1])
	if err != nil {
		return VCPUEntry{}, fmt.Errorf("failed to parse VCPU ID: %v", err)
	}

	cpu := -1
	if fields[offset+2] != "-" {
		cpu, err = strconv.Atoi(fields[offset+2])
		if err != nil {
			return VCPUEntry{}, fmt.Errorf("failed to parse CPU: %v", err)
		}
	}

	// Parse state
	state := fields[offset+3]
	if len(state) != 3 {
		return VCPUEntry{}, fmt.Errorf("invalid state format: %s", state)
	}

	// Parse time
	timeSeconds, err := strconv.ParseFloat(fields[offset+4], 64)
	if err != nil {
		return VCPUEntry{}, fmt.Errorf("failed to parse time: %v", err)
	}

	// Parse affinity
	affinity := strings.Join(fields[offset+5:], " ")
	parts := strings.Split(affinity, " / ")
	hardAffinity := parts[0]
	softAffinity := ""
	if len(parts) > 1 {
		softAffinity = parts[1]
	}

	return VCPUEntry{
		DomainName:    domainName,
		DomainID:     domainID,
		VCPUID:       vcpuid,
		CPU:          cpu,
		State:        state,
		TimeSeconds:  timeSeconds,
		HardAffinity: hardAffinity,
		SoftAffinity: softAffinity,
	}, nil
}

func main() {
	// Sample data from the test file
	testLines := []string{
		"Name                              ID  VCPU  CPU  State  Time(s)  Affinity (Hard / Soft)",
		"Domain-1                          1     0     1  r--     123.4   0-3 / 0-1",
		"Domain-1                          1     1    -  ---      45.6   0-3 / 2-3",
		"Domain-1                          1     2     3  -b-     200.0   0-3 / 0-1",
		"Domain-2                          2     0     0  r--     789.0   all / 0",
		"Domain-2                          2     1     1  -b-     500.5   all / 1",
		"Test-Domain                       4     0    -  ---       0.0   0-7 / 0-3",
		"Test-Domain                       4     1     2  r--     150.2   0-7 / 4-7",
		"vm-ubuntu                         5     0     0  r--    1000.0   0,2-3,5 / 1,3",
		"vm-ubuntu                         5     1     1  -b-     800.0   0,2-3,5 / 2,4",
		"pv-guest                          6     0     3  r--      50.0   0 / 0",
		"pv-guest                          6     1     2  -b-      30.0   1 / 1",
	}

	fmt.Println("Testing VCPU line parsers...")
	fmt.Println("============================")

	for i, line := range testLines {
		if i == 0 {
			continue // Skip header
		}

		fmt.Printf("\nTest line %d: %s\n", i, line)

		// Test old parser
		oldEntry, oldErr := parseVcpuLineOld(line)
		if oldErr != nil {
			fmt.Printf("  Old parser ERROR: %v\n", oldErr)
		} else {
			fmt.Printf("  Old parser OK: %+v\n", oldEntry)
		}

		// Test new parser
		newEntry, newErr := parseVcpuLineNew(line)
		if newErr != nil {
			fmt.Printf("  New parser ERROR: %v\n", newErr)
		} else {
			fmt.Printf("  New parser OK: %+v\n", newEntry)
		}

		// Compare results
		if oldErr == nil && newErr == nil {
			if oldEntry.DomainName == newEntry.DomainName &&
			   oldEntry.DomainID == newEntry.DomainID &&
			   oldEntry.VCPUID == newEntry.VCPUID &&
			   oldEntry.CPU == newEntry.CPU &&
			   oldEntry.State == newEntry.State &&
			   oldEntry.TimeSeconds == newEntry.TimeSeconds &&
			   oldEntry.HardAffinity == newEntry.HardAffinity &&
			   oldEntry.SoftAffinity == newEntry.SoftAffinity {
				fmt.Printf("  ✓ Results match!\n")
			} else {
				fmt.Printf("  ✗ Results differ!\n")
			}
		}
	}
}
package pedestal

import (
	"strconv"
	"strings"
)

// LinuxResource.CPU.CPUs is a comma-separated list, with dashes to represent ranges.
// 0-3,7 represents CPUs 0,1,2,3, and 7. we store them into an array
// dot no need to consider the disorder case, because containerd guaranteeses it
// hence, the return will not be nil
func ParseOCICPUString(cpuset string) []int {
	if cpuset == "" {
		return []int{}
	}

	// For small ranges (most common case), use a simple approach
	// Pre-allocate with small capacity optimized for typical CPU sets
	cpuarr := make([]int, 0, 8) // Most CPU sets are small (1-8 CPUs)

	parts := strings.Split(cpuset, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				startStr := strings.TrimSpace(rangeParts[0])
				endStr := strings.TrimSpace(rangeParts[1])
				start, err1 := strconv.Atoi(startStr)
				end, err2 := strconv.Atoi(endStr)
				if err1 == nil && err2 == nil && start <= end {
					for i := start; i <= end; i++ {
						cpuarr = append(cpuarr, i)
					}
				}
			}
		} else {
			if cpuNum, err := strconv.Atoi(part); err == nil {
				cpuarr = append(cpuarr, cpuNum)
			}
		}
	}
	return cpuarr
}

// ParseCPUArr translates CPU array to CPU string format
// Example: [1,4,5] -> "1,4-5"
func ParseCPUArr(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}

	// Sort the CPU array
	sorted := make([]int, len(cpus))
	copy(sorted, cpus)

	// Simple bubble sort for small arrays
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	// Convert to string format
	var result strings.Builder
	start := sorted[0]
	end := sorted[0]

	for i := 1; i < len(sorted); i++ {
		if sorted[i] == end+1 {
			// Continue the range
			end = sorted[i]
		} else {
			// End the current range and start a new one
			if start == end {
				result.WriteString(strconv.Itoa(start))
			} else {
				result.WriteString(strconv.Itoa(start))
				result.WriteString("-")
				result.WriteString(strconv.Itoa(end))
			}
			result.WriteString(",")
			start = sorted[i]
			end = sorted[i]
		}
	}

	// Add the last range
	if start == end {
		result.WriteString(strconv.Itoa(start))
	} else {
		result.WriteString(strconv.Itoa(start))
		result.WriteString("-")
		result.WriteString(strconv.Itoa(end))
	}

	return result.String()
}

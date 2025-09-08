package pedestal

import (
	"reflect"
	"testing"
)

func TestParseOCICPUString(t *testing.T) {
	tests := []struct {
		name     string
		cpuset   string
		expected []int
	}{
		{
			name:     "single CPU",
			cpuset:   "0",
			expected: []int{0},
		},
		{
			name:     "CPU range",
			cpuset:   "0-3",
			expected: []int{0, 1, 2, 3},
		},
		{
			name:     "complex CPU set",
			cpuset:   "0-3,7",
			expected: []int{0, 1, 2, 3, 7},
		},
		{
			name:     "comma separated",
			cpuset:   "0,2,4",
			expected: []int{0, 2, 4},
		},
		{
			name:     "multiple ranges",
			cpuset:   "0-1,3-5,7",
			expected: []int{0, 1, 3, 4, 5, 7},
		},
		{
			name:     "single range element",
			cpuset:   "5-5",
			expected: []int{5},
		},
		{
			name:     "larger numbers",
			cpuset:   "10-12,15",
			expected: []int{10, 11, 12, 15},
		},
		{
			name:     "with whitespace",
			cpuset:   " 0-3 , 7 ",
			expected: []int{0, 1, 2, 3, 7},
		},
		{
			name:     "mixed format",
			cpuset:   "1-2,4-4,7",
			expected: []int{1, 2, 4, 7},
		},
		{
			name:     "empty string",
			cpuset:   "",
			expected: []int{}, // should return empty slice, never nil
		},
		{
			name:     "invalid range start",
			cpuset:   "a-3",
			expected: []int{}, // invalid elements are skipped
		},
		{
			name:     "invalid range end",
			cpuset:   "0-b",
			expected: []int{}, // invalid elements are skipped
		},
		{
			name:     "invalid single CPU",
			cpuset:   "abc",
			expected: []int{}, // invalid elements are skipped
		},
		{
			name:     "partial valid mixed with invalid",
			cpuset:   "0-1,invalid,3",
			expected: []int{0, 1, 3}, // invalid elements are skipped, valid ones kept
		},
		{
			name:     "large range",
			cpuset:   "100-105",
			expected: []int{100, 101, 102, 103, 104, 105},
		},
		{
			name:     "zero CPU",
			cpuset:   "0",
			expected: []int{0},
		},
		{
			name:     "high CPU numbers",
			cpuset:   "255,256-258",
			expected: []int{255, 256, 257, 258},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseOCICPUString(tt.cpuset)

			// Never nil check
			if result == nil {
				t.Fatalf("ParseOCICPUString() returned nil, expected non-nil slice")
			}

			// Compare slices (order should be preserved as per implementation)
			if len(result) != len(tt.expected) {
				t.Errorf("ParseOCICPUString() = %v, expected %v", result, tt.expected)
				return
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("ParseOCICPUString() = %v, expected %v", result, tt.expected)
					return
				}
			}
		})
	}
}

func TestParseCPUArr(t *testing.T) {
	tests := []struct {
		name     string
		cpus     []int
		expected string
	}{
		{
			name:     "empty slice",
			cpus:     []int{},
			expected: "",
		},
		{
			name:     "single element",
			cpus:     []int{1},
			expected: "1",
		},
		{
			name:     "consecutive range",
			cpus:     []int{1, 2, 3},
			expected: "1-3",
		},
		{
			name:     "mixed range and single",
			cpus:     []int{1, 2, 3, 5, 7},
			expected: "1-3,5,7",
		},
		{
			name:     "complex ranges",
			cpus:     []int{0, 1, 3, 4, 5, 7, 9, 10},
			expected: "0-1,3-5,7,9-10",
		},
		{
			name:     "single element at end",
			cpus:     []int{0, 1, 2, 5},
			expected: "0-2,5",
		},
		{
			name:     "all single elements",
			cpus:     []int{1, 3, 5, 7},
			expected: "1,3,5,7",
		},
		{
			name:     "large numbers",
			cpus:     []int{100, 101, 102, 105},
			expected: "100-102,105",
		},
		{
			name:     "unsorted input",
			cpus:     []int{7, 3, 5, 1, 2},
			expected: "1-3,5,7", // Bubble sort will sort it
		},
		{
			name:     "zero CPU",
			cpus:     []int{0, 1, 2},
			expected: "0-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCPUArr(tt.cpus)
			if result != tt.expected {
				t.Errorf("ParseCPUArr() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// Test round-trip consistency between ParseOCICPUString and ParseCPUArr
func TestParseRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		cpuset string
	}{
		{"simple range", "0-3"},
		{"complex", "0-2,4-6,8"},
		{"singles only", "1,3,5"},
		{"mixed", "0-1,3,5-7"},
		{"large", "100-105,200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse string to slice
			cpuSlice := ParseOCICPUString(tt.cpuset)

			// Parse slice back to string
			resultString := ParseCPUArr(cpuSlice)

			// They should match (accounting for sorting)
			if !compareCPUStrings(tt.cpuset, resultString) {
				t.Errorf("Round-trip failed: input %q -> slice %v -> output %q", tt.cpuset, cpuSlice, resultString)
			}
		})
	}
}

// Helper function to compare CPU strings accounting for equivalent formats
func compareCPUStrings(str1, str2 string) bool {
	if str1 == str2 {
		return true
	}

	// Convert both to sorted CPU arrays and compare
	arr1 := ParseOCICPUString(str1)
	arr2 := ParseOCICPUString(str2)

	return reflect.DeepEqual(arr1, arr2)
}

func TestParseOCICPUStringEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		cpuset   string
		expected []int
	}{
		{
			name:     "maximum reasonable range",
			cpuset:   "0-63", // 64 CPUs
			expected: getRange(0, 63),
		},
		{
			name:     "very large CPUs",
			cpuset:   "512,1023",
			expected: []int{512, 1023},
		},
		{
			name:     "range with zero overlap",
			cpuset:   "0-5,5-10",
			expected: []int{0, 1, 2, 3, 4, 5, 5, 6, 7, 8, 9, 10}, // Should contain duplicates as per current impl
		},
		{
			name:     "repeated single CPU",
			cpuset:   "1,1,2,2",
			expected: []int{1, 1, 2, 2}, // Should contain duplicates as per current impl
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseOCICPUString(tt.cpuset)

			if result == nil {
				t.Fatalf("ParseOCICPUString() returned nil for %q", tt.cpuset)
			}

			if len(result) != len(tt.expected) {
				t.Errorf("ParseOCICPUString(%q) length = %d, expected %d", tt.cpuset, len(result), len(tt.expected))
				t.Errorf("     result: %v", result)
				t.Errorf("     expected: %v", tt.expected)
				return
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("ParseOCICPUString(%q)[%d] = %d, expected %d", tt.cpuset, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func getRange(start, end int) []int {
	result := make([]int, end-start+1)
	for i := start; i <= end; i++ {
		result[i-start] = i
	}
	return result
}

package cntr

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"time"
)

var (
	cpuprofile      = flag.String("test.cpuprofile", "", "write cpu profile to file")
	memprofile      = flag.String("test.memprofile", "", "write memory profile to file")
	profileDuration = flag.Duration("profile.duration", 30*time.Second, "duration for profiling before exit")
)

// ProfileConfig holds profiling configuration
type ProfileConfig struct {
	CPUFilename    string
	MemoryFilename string
	Duration       time.Duration
}

// RunWithProfiling runs a function with CPU and memory profiling enabled
func RunWithProfiling(fn func(), config *ProfileConfig) {
	if config == nil {
		config = &ProfileConfig{
			CPUFilename:    "cntr_cpu.prof",
			MemoryFilename: "cntr_mem.prof",
			Duration:       30 * time.Second,
		}
	}

	fmt.Printf("🔥 Starting cntr package profiling...\n")

	// CPU profiling
	if config.CPUFilename != "" {
		fmt.Printf("📊 CPU profiling enabled, output: %s\n", config.CPUFilename)
		f, err := os.Create(config.CPUFilename)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	// Memory profiling setup
	if config.MemoryFilename != "" {
		fmt.Printf("🧠 Memory profiling enabled, output: %s\n", config.MemoryFilename)
	}

	// Run the target function
	start := time.Now()
	fn()
	elapsed := time.Since(start)

	// Memory profiling
	if config.MemoryFilename != "" {
		f, err := os.Create(config.MemoryFilename)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer f.Close()

		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
		fmt.Printf("✅ Memory profile saved: %s\n", config.MemoryFilename)
	}

	if config.CPUFilename != "" {
		fmt.Printf("✅ CPU profile saved: %s\n", config.CPUFilename)
	}

	fmt.Printf("🎯 cntr profiling complete! Duration: %v\n", elapsed)
}

// ParseProfilingFlags parses command line flags for profiling
func ParseProfilingFlags() *ProfileConfig {
	flag.Parse()

	if *cpuprofile == "" && *memprofile == "" {
		return nil
	}

	return &ProfileConfig{
		CPUFilename:    *cpuprofile,
		MemoryFilename: *memprofile,
		Duration:       *profileDuration,
	}
}

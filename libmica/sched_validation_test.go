//go:build ignore
// +build ignore

package libmica

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// Include the scheduler implementation directly for testing
// (This is a temporary validation program)

func TestSchedValidation() {
	fmt.Println("=== MICA Sched Shared Memory Validation ===")
	
	// Show system info
	fmt.Printf("System info: shmData=%p, maxCPUs=%d\n", shmData, maxCPUs)
	
	if shmData == nil {
		fmt.Println("❌ ERROR: Shared memory not initialized")
		return
	}
	
	// Check initial state
	shmData.lock()
	initialCount := shmData.count()
	isEmpty := shmData.isEmpty()
	shmData.unlock()
	
	fmt.Printf("Initial state: count=%d, isEmpty=%t, maxCPUs=%d\n", initialCount, isEmpty, maxCPUs)
	
	if initialCount != maxCPUs {
		fmt.Printf("⚠️  WARNING: Expected %d CPUs initially, got %d\n", maxCPUs, initialCount)
	}
	
	// Test 1: Basic allocation and release
	fmt.Println("\n=== Test 1: Basic Operations ===")
	
	cpu := schedFreeCPU()
	if cpu == 0 {
		fmt.Println("❌ FAIL: Could not allocate CPU")
		return
	}
	fmt.Printf("✅ Allocated CPU %d\n", cpu)
	
	shmData.lock()
	countAfterAlloc := shmData.count()
	shmData.unlock()
	fmt.Printf("Queue size after allocation: %d\n", countAfterAlloc)
	
	releaseUsedCPU(cpu)
	fmt.Printf("✅ Released CPU %d\n", cpu)
	
	shmData.lock()
	countAfterRelease := shmData.count()
	shmData.unlock()
	fmt.Printf("Queue size after release: %d\n", countAfterRelease)
	
	// Test 2: Release priority
	fmt.Println("\n=== Test 2: Release Priority ===")
	
	if maxCPUs >= 3 {
		cpu1 := schedFreeCPU()
		cpu2 := schedFreeCPU()
		cpu3 := schedFreeCPU()
		
		if cpu1 != 0 && cpu2 != 0 && cpu3 != 0 {
			fmt.Printf("Allocated CPUs: %d, %d, %d\n", cpu1, cpu2, cpu3)
			
			// Release in specific order to test priority
			releaseUsedCPU(cpu1)    // goes to tail
			releaseUnusedCPU(cpu2)  // goes to head  
			releaseUsedCPU(cpu3)    // goes to tail
			
			// Next allocation should get cpu2 (unused CPU priority)
			nextCPU := schedFreeCPU()
			if nextCPU == cpu2 {
				fmt.Printf("✅ Priority test passed: got unused CPU %d first\n", nextCPU)
			} else {
				fmt.Printf("⚠️  Priority test warning: expected CPU %d, got %d\n", cpu2, nextCPU)
			}
			
			// Clean up
			if nextCPU != 0 {
				releaseUsedCPU(nextCPU)
			}
		} else {
			fmt.Println("❌ Could not allocate 3 CPUs for priority test")
		}
	} else {
		fmt.Println("⏭️  Skipping priority test (need at least 3 CPUs)")
	}
	
	// Test 3: Concurrent stress test
	fmt.Println("\n=== Test 3: Concurrent Stress Test ===")
	
	var (
		allocCount   int64
		releaseCount int64
		errors       int64
	)
	
	const (
		numWorkers  = 8
		iterations  = 100
	)
	
	var wg sync.WaitGroup
	start := time.Now()
	
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for j := 0; j < iterations; j++ {
				cpu := schedFreeCPU()
				if cpu == 0 {
					atomic.AddInt64(&errors, 1)
					time.Sleep(1 * time.Millisecond)
					continue
				}
				
				atomic.AddInt64(&allocCount, 1)
				
				// Brief simulation of work
				time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
				
				// Random release strategy
				if rand.Float32() < 0.5 {
					releaseUsedCPU(cpu)
				} else {
					releaseUnusedCPU(cpu)
				}
				atomic.AddInt64(&releaseCount, 1)
			}
		}(i)
	}
	
	wg.Wait()
	duration := time.Since(start)
	
	fmt.Printf("Stress test completed in %v\n", duration)
	fmt.Printf("  Allocations: %d\n", allocCount)
	fmt.Printf("  Releases: %d\n", releaseCount)
	fmt.Printf("  Errors: %d\n", errors)
	
	// Final state check
	shmData.lock()
	finalCount := shmData.count()
	shmData.unlock()
	
	fmt.Printf("  Final queue size: %d\n", finalCount)
	
	if allocCount == releaseCount {
		fmt.Println("✅ Allocation/release count matches")
	} else {
		fmt.Printf("⚠️  Allocation/release mismatch: %d vs %d\n", allocCount, releaseCount)
	}
	
	if finalCount == maxCPUs {
		fmt.Println("✅ Final queue size correct")
	} else {
		fmt.Printf("⚠️  Final queue size incorrect: expected %d, got %d\n", maxCPUs, finalCount)
	}
	
	fmt.Println("\n=== Validation Summary ===")
	
	success := true
	if allocCount != releaseCount {
		fmt.Println("❌ FAIL: Allocation/release count mismatch")
		success = false
	}
	if finalCount != maxCPUs {
		fmt.Println("❌ FAIL: Final queue size incorrect")
		success = false
	}
	
	if success {
		fmt.Println("🎉 All validations passed!")
	} else {
		fmt.Println("⚠️  Some validations failed")
	}
} 
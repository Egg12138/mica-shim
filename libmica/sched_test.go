package libmica

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test statistics
var (
	testAllocatedCPUs      int64
	testReleasedUnusedCPUs int64
	testReleasedUsedCPUs   int64
)

// Wrapper functions to track statistics while using internal functions
func testSchedFreeCPU() uint32 {
	fmt.Println("shmData", shmData)
	cpu := schedFreeCPU()
	if cpu != 0 {
		fmt.Println("testAllocatedCPUs", atomic.LoadInt64(&testAllocatedCPUs))
		atomic.AddInt64(&testAllocatedCPUs, 1)
	}
	return cpu
}

func testReleaseUnusedCPU(cpu uint32) {
	releaseUnusedCPU(cpu)
	atomic.AddInt64(&testReleasedUnusedCPUs, 1)
}

func testReleaseUsedCPU(cpu uint32) {
	releaseUsedCPU(cpu)
	atomic.AddInt64(&testReleasedUsedCPUs, 1)
}

// Test basic queue operations
func TestBasicQueueOperations(t *testing.T) {
	if shmData == nil {
		t.Fatal("shared memory not initialized")
	}

	// Test basic allocation and release
	cpu := testSchedFreeCPU()
	if cpu == 0 {
		t.Fatal("failed to allocate CPU")
	}
	fmt.Println("shmData", shmData)

	// Release as used CPU
	testReleaseUsedCPU(cpu)

	fmt.Println("shmData", shmData)
	// Verify queue state
	shmData.lock()
	count := shmData.count()
	shmData.unlock()

	if count != maxCPUs {
		t.Errorf("expected %d CPUs in queue, got %d", maxCPUs, count)
	}
}

// Test queue empty/full conditions
func TestQueueBounds(t *testing.T) {
	if shmData == nil {
		t.Fatal("shared memory not initialized")
	}

	var allocatedCPUs []uint32

	// Allocate all CPUs
	for i := uint32(0); i < maxCPUs; i++ {
		cpu := testSchedFreeCPU()
		if cpu == 0 {
			t.Fatalf("failed to allocate CPU %d, should have %d CPUs available", i+1, maxCPUs)
		}
		allocatedCPUs = append(allocatedCPUs, cpu)
	}

	// Verify queue is empty
	shmData.lock()
	isEmpty := shmData.isEmpty()
	shmData.unlock()

	if !isEmpty {
		t.Error("queue should be empty after allocating all CPUs")
	}

	// Try to allocate one more (should fail)
	cpu := testSchedFreeCPU()
	if cpu != 0 {
		t.Error("should not be able to allocate CPU when queue is empty")
	}

	// Release all CPUs
	for _, cpu := range allocatedCPUs {
		testReleaseUsedCPU(cpu)
	}

	// Verify all CPUs are back
	shmData.lock()
	finalCount := shmData.count()
	shmData.unlock()

	if finalCount != maxCPUs {
		t.Errorf("expected %d CPUs after release, got %d", maxCPUs, finalCount)
	}
}

// Test priority of unused vs used CPU releases
func TestReleasePriority(t *testing.T) {
	if maxCPUs < 3 {
		t.Skip("need at least 3 CPUs for this test")
	}

	// Allocate 3 CPUs
	cpu1 := testSchedFreeCPU()
	cpu2 := testSchedFreeCPU()
	cpu3 := testSchedFreeCPU()

	if cpu1 == 0 || cpu2 == 0 || cpu3 == 0 {
		t.Fatal("failed to allocate 3 CPUs")
	}

	// Release cpu1 as used (goes to tail)
	testReleaseUsedCPU(cpu1)
	// Release cpu2 as unused (goes to head)
	testReleaseUnusedCPU(cpu2)
	// Release cpu3 as used (goes to tail)
	testReleaseUsedCPU(cpu3)

	// Next allocation should get cpu2 (unused CPU has priority)
	nextCPU := testSchedFreeCPU()
	if nextCPU != cpu2 {
		t.Errorf("expected to get unused CPU %d first, got %d", cpu2, nextCPU)
	}

	// Clean up
	testReleaseUsedCPU(nextCPU)
}

// Simulate RTOS client behavior for concurrent testing
func simulateRTOSClient(t *testing.T, clientID int, wg *sync.WaitGroup, iterations int) {
	defer wg.Done()

	for i := 0; i < iterations; i++ {
		// Request CPU
		cpu := testSchedFreeCPU()
		if cpu == 0 {
			// No CPU available, continue
			time.Sleep(1 * time.Millisecond)
			continue
		}

		// Simulate RTOS runtime
		time.Sleep(time.Duration(rand.Intn(5)+1) * time.Millisecond)

		// Randomly decide release strategy
		if rand.Float32() < 0.7 { // 70% chance to use CPU
			testReleaseUsedCPU(cpu)
		} else { // 30% chance to release without using
			testReleaseUnusedCPU(cpu)
		}
	}
}

// Test concurrent access
func TestConcurrentAccess(t *testing.T) {
	if shmData == nil {
		t.Fatal("shared memory not initialized")
	}

	// Reset test statistics
	atomic.StoreInt64(&testAllocatedCPUs, 0)
	atomic.StoreInt64(&testReleasedUnusedCPUs, 0)
	atomic.StoreInt64(&testReleasedUsedCPUs, 0)

	const numClients = 8
	const iterations = 50

	var wg sync.WaitGroup

	// Start multiple concurrent clients
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go simulateRTOSClient(t, i, &wg, iterations)
	}

	wg.Wait()

	// Verify statistics
	allocated := atomic.LoadInt64(&testAllocatedCPUs)
	releasedUnused := atomic.LoadInt64(&testReleasedUnusedCPUs)
	releasedUsed := atomic.LoadInt64(&testReleasedUsedCPUs)

	t.Logf("Concurrent test results:")
	t.Logf("  Total allocations: %d", allocated)
	t.Logf("  Released unused: %d", releasedUnused)
	t.Logf("  Released used: %d", releasedUsed)
	t.Logf("  Total releases: %d", releasedUnused+releasedUsed)

	// Check that allocations and releases match
	if allocated != releasedUnused+releasedUsed {
		t.Errorf("allocation/release mismatch: allocated=%d, released=%d", allocated, releasedUnused+releasedUsed)
	}

	// Verify final queue state
	shmData.lock()
	finalCount := shmData.count()
	shmData.unlock()

	if finalCount != maxCPUs {
		t.Errorf("expected %d CPUs in final queue, got %d", maxCPUs, finalCount)
	}
}

// Benchmark CPU allocation
func BenchmarkSchedFreeCPU(b *testing.B) {
	if shmData == nil {
		b.Fatal("shared memory not initialized")
	}

	// Pre-allocate slice to avoid allocation overhead in benchmark
	allocated := make([]uint32, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cpu := schedFreeCPU()
		allocated[i] = cpu
		if cpu == 0 {
			// Release some CPUs to continue
			for j := 0; j < i; j++ {
				if allocated[j] != 0 {
					releaseUsedCPU(allocated[j])
					allocated[j] = 0
				}
			}
			cpu = schedFreeCPU()
			allocated[i] = cpu
		}
	}
	b.StopTimer()

	// Clean up
	for i := 0; i < b.N; i++ {
		if allocated[i] != 0 {
			releaseUsedCPU(allocated[i])
		}
	}
}

// Benchmark CPU release
func BenchmarkReleaseUsedCPU(b *testing.B) {
	if shmData == nil {
		b.Fatal("shared memory not initialized")
	}

	// Pre-allocate CPUs
	allocated := make([]uint32, b.N)
	for i := 0; i < b.N; i++ {
		allocated[i] = schedFreeCPU()
		if allocated[i] == 0 {
			b.Fatalf("failed to pre-allocate CPU %d", i)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		releaseUsedCPU(allocated[i])
	}
}

// Test comprehensive scenario
func TestComprehensiveScenario(t *testing.T) {
	if shmData == nil {
		t.Fatal("shared memory not initialized")
	}

	t.Log("Starting comprehensive CPU scheduler test")

	// Reset statistics
	atomic.StoreInt64(&testAllocatedCPUs, 0)
	atomic.StoreInt64(&testReleasedUnusedCPUs, 0)
	atomic.StoreInt64(&testReleasedUsedCPUs, 0)

	// Test 1: Sequential operations
	t.Log("Phase 1: Sequential operations")
	for i := 0; i < 10; i++ {
		cpu := testSchedFreeCPU()
		if cpu == 0 {
			t.Fatal("failed sequential allocation")
		}
		if i%2 == 0 {
			testReleaseUsedCPU(cpu)
		} else {
			testReleaseUnusedCPU(cpu)
		}
	}

	// Test 2: Burst allocation and release
	t.Log("Phase 2: Burst operations")
	var burst []uint32
	for i := uint32(0); i < maxCPUs/2; i++ {
		cpu := testSchedFreeCPU()
		if cpu != 0 {
			burst = append(burst, cpu)
		}
	}
	for _, cpu := range burst {
		testReleaseUsedCPU(cpu)
	}

	// Test 3: Concurrent stress test
	t.Log("Phase 3: Concurrent stress test")
	const stressClients = 5
	const stressIterations = 20

	var wg sync.WaitGroup
	for i := 0; i < stressClients; i++ {
		wg.Add(1)
		go simulateRTOSClient(t, i, &wg, stressIterations)
	}
	wg.Wait()

	// Final verification
	allocated := atomic.LoadInt64(&testAllocatedCPUs)
	releasedUnused := atomic.LoadInt64(&testReleasedUnusedCPUs)
	releasedUsed := atomic.LoadInt64(&testReleasedUsedCPUs)

	t.Logf("Final statistics:")
	t.Logf("  Total allocations: %d", allocated)
	t.Logf("  Released unused: %d", releasedUnused)
	t.Logf("  Released used: %d", releasedUsed)

	if allocated != releasedUnused+releasedUsed {
		t.Errorf("final allocation/release mismatch: allocated=%d, released=%d",
			allocated, releasedUnused+releasedUsed)
	}

	shmData.lock()
	finalCount := shmData.count()
	shmData.unlock()

	if finalCount != maxCPUs {
		t.Errorf("final queue verification failed: expected %d CPUs, got %d", maxCPUs, finalCount)
	}

	t.Log("Comprehensive test completed successfully")
}

package libmica

import (
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

type FreeCPUs struct {
	// ptr
	head uint32
	tail uint32
	size uint32
	m    sync.Mutex
	cpus [64]uint32
}

var (
	shmData *FreeCPUs
	maxCPUs uint32
)

const SHM_SIZE = int(unsafe.Sizeof(FreeCPUs{}))

func (q *FreeCPUs) lock() {
	q.m.Lock()
}

func (q *FreeCPUs) unlock() {
	q.m.Unlock()
}

func (q *FreeCPUs) isEmpty() bool {
	return q.count() == 0
}

func (q *FreeCPUs) count() uint32 {
	if q.tail >= q.head {
		return q.tail - q.head
	}
	return q.size - q.head + q.tail
}

func initSharedMemory(create bool) error {
	flags := os.O_RDWR
	if create {
		flags |= syscall.O_CREAT
	}
	fd, err := os.OpenFile(defs.SHM_NAME, flags, 0666)
	if err != nil {
		log.Errorf("failed to open shared memory: %v", err)
		return err
	}
	defer fd.Close()

	if create {
		if err := fd.Truncate(int64(SHM_SIZE)); err != nil {
			log.LocateDebugf("failed to truncate shared memory: %v", err)
			return err
		}
	}

	prot := syscall.PROT_READ | syscall.PROT_WRITE
	data, err := syscall.Mmap(int(fd.Fd()), 0, SHM_SIZE, prot, syscall.MAP_SHARED)
	if err != nil {
		log.LocateDebugf("failed to mmap shared memory: %v", err)
		return err
	}

	shmData = (*FreeCPUs)(unsafe.Pointer(&data[0]))

	if create {
		// TODO: getting max CPUs from ?
		maxCPUs = uint32(runtime.NumCPU())
		shmData.head = 0
		shmData.tail = 0
		shmData.size = maxCPUs

		for i := uint32(0); i < maxCPUs; i++ {
			shmData.cpus[i] = i
		}
		shmData.tail = maxCPUs

		log.Debugf("initialized CPU queue with %d CPUs", maxCPUs)
	} else {
		maxCPUs = shmData.size
	}

	return nil
}

// lazy init CPUFrequencyMap
func init() {
	err := initSharedMemory(false)
	if err != nil {
		err = initSharedMemory(true)
		if err != nil {
			log.Fatalf("failed to init shared memory: %v", err)
		}
	}
}

func (q *FreeCPUs) dequeue() (uint32, bool) {
	if q.head == q.tail {
		return 0, false
	}

	cpu := q.cpus[q.head]
	q.head = (q.head + 1) % q.size
	return cpu, true
}

func (q *FreeCPUs) enqueueHead(cpu uint32) bool {
	newHead := (q.head - 1 + q.size) % q.size
	if newHead == q.tail {
		return false
	}

	q.head = newHead
	q.cpus[q.head] = cpu
	return true
}

func (q *FreeCPUs) enqueueTail(cpu uint32) bool {
	newTail := (q.tail + 1) % q.size
	if newTail == q.head {
		return false
	}

	q.cpus[q.tail] = cpu
	q.tail = newTail
	return true
}

func schedFreeCPU() uint32 {
	if shmData == nil {
		log.Errorf("shared memory not initialized")
		return 0
	}

	shmData.lock()
	defer shmData.unlock()

	cpu, ok := shmData.dequeue()
	if !ok {
		log.Errorf("no free CPU available")
		return 0
	}

	log.Debugf("allocated CPU %d", cpu)
	return cpu
}

func releaseUnusedCPU(cpu uint32) {
	if shmData == nil {
		log.Errorf("shared memory not initialized")
		return
	}

	shmData.lock()
	defer shmData.unlock()

	if !shmData.enqueueHead(cpu) {
		log.Errorf("failed to release unused CPU %d: queue full", cpu)
		return
	}

	log.Debugf("released unused CPU %d to head", cpu)
}

func releaseUsedCPU(cpu uint32) {
	if shmData == nil {
		log.Errorf("shared memory not initialized")
		return
	}

	shmData.lock()
	defer shmData.unlock()

	if !shmData.enqueueTail(cpu) {
		log.Errorf("failed to release used CPU %d: queue full", cpu)
		return
	}

	log.Debugf("released used CPU %d to tail", cpu)
}

// get CPU number the RTOS wanna take
func getNCPU(bundle string) uint32 {
	// TODO: 现在我们全部假定是单核RTOS, mica侧还未实现多核, 但是在镜像label中，我们可以指定核数量
	// 1. search bundle/.../<clientOSname>.elf
	// 2. if missing, log and search for binary in bundle recursively
	return 1
}

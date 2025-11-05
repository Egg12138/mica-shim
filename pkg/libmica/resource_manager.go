package libmica

import (
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/pedestal"
	"strconv"
	"strings"
)

const cpuCapRatio = 100

// TODO: this function is not in use now, we migrate from `UpdateVCPUs` to this function
// after allocating sandbox cpus in xen pool is finished.
func (me *MicaExecutor) UpdateSandboxPoolVCPUs() {}

func (me *MicaExecutor) MemoryThresholdMB() uint32 {
	return me.memoryThresholdMB
}

func (me *MicaExecutor) CurrentMemoryMB() uint32 {
	if me.records.memoryMB <= 0 {
		return 0
	}
	return uint32(me.records.memoryMB)
}

func (me *MicaExecutor) RecordMemoryState(current, threshold uint32) {
	me.records.memoryMB = int(current)
	if threshold == 0 {
		threshold = current
	}
	me.memoryThresholdMB = max(me.memoryThresholdMB, threshold)
}

// EnsureMemoryLimit applies the requested memory limit, expanding the pedestal maximum first when needed.
func (me *MicaExecutor) EnsureMemoryLimit(target uint32) error {
	current := me.CurrentMemoryMB()
	threshold := me.MemoryThresholdMB()

	if threshold == 0 {
		threshold = current
	}

	if threshold < target {
		if err := me.UpdateMemoryPedMax(target); err != nil {
			return err
		}
		me.memoryThresholdMB = target
	}

	if current == target {
		return nil
	}

	if err := me.UpdateMemory(target); err != nil {
		return err
	}

	return nil
}

// number of visible vcpus
func (me *MicaExecutor) UpdateVCPUNum(newVCPUs uint32) (oldCPUs, newCPUs uint32, retErr error) {
	log.Debugf("UpdateVCPUNum: container=%s, old=%d, new=%d", me.Id, me.records.vcpuNum, newVCPUs)
	cmdArgs := []string{"VCPU", strconv.Itoa(int(newVCPUs))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to update vcpu number: %v", err)
		return uint32(me.records.vcpuNum), uint32(me.records.vcpuNum), err
	}
	return uint32(me.records.vcpuNum), newVCPUs, err
}

// TODO: temporarily dirty-join string as command line, need to change to a better way
func (me *MicaExecutor) UpdatePCPUConstrains(cpus string) error {
	log.Debugf("UpdatePCPUConstrains: container=%s, cpuset=%s", me.Id, cpus)
	cmdArgs := []string{"CPU", cpus}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to bind physical cpuset \"%s\" to container: %v", cpus, err)
	} else {
		me.records.cpuStr = [MaxCPUStringLen]byte{}
		log.Debugf("updated to new cpuset: %s", cpus)
	}
	return err
}

func (me *MicaExecutor) UpdateCPUCapacity(cap uint32) error {
	log.Debugf("UpdateCPUCapacity: container=%s, old=%d, new=%d", me.Id, me.records.cpuCapacity, cap)
	cmdArgs := []string{"CPUCpacity", strconv.Itoa(int(cap))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to update cap time to %d that container can run: %v", cap, err)
	} else {
		me.records.cpuCapacity = int(cap)
		log.Debugf("updated to new cpu capacity: %d", cap)
	}
	return err
}

func (me *MicaExecutor) UpdateCPUWeight(weight uint32) error {
	log.Debugf("UpdateCPUWeight: old=%d, new=%d", me.Id, me.records.cpuWeight, weight)
	cmdArgs := []string{"CPUWeight", strconv.Itoa(int(weight))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to update cpu share time to %d that container can run: %v", weight, err)
	} else {
		me.records.cpuWeight = int(weight)
		log.Debugf("updated to new cpu weight: %d", weight)
	}
	return err
}

// NOTICE: MemoryLimit is not max memory of client.It is the max memory
// that pedestal can allocate to container.
// Memory is just the max memory of a client
func (me *MicaExecutor) UpdateMemoryPedMax(memMiB uint32) error {
	log.Debugf("UpdateMemoryPedMax: container=%s, old=%d, new=%d", me.Id, me.records.memoryMB, memMiB)
	cmdArgs := []string{"MaxMem", strconv.Itoa(int(memMiB))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to request new max memory \"%d\" to container: %v", memMiB, err)
	} else {
		me.memoryThresholdMB = max(memMiB, me.memoryThresholdMB)
		log.Debugf("update max memory threshold to %d", memMiB)
	}
	return err
}

func (me *MicaExecutor) UpdateMemory(memMiB uint32) error {
	log.Debugf("UpdateMemory: container=%s, old=%d, new=%d", me.Id, me.records.memoryMB, memMiB)
	cmdArgs := []string{"Memory", strconv.Itoa(int(memMiB))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to request new memory \"%d\" to container: %v", memMiB, err)
	} else {
		me.records.memoryMB = int(memMiB)
		me.memoryThresholdMB = max(memMiB, me.memoryThresholdMB)
		log.Debugf("update memory to %d", memMiB)
	}
	return err
}

func (me *MicaExecutor) ReadResource() *pedestal.EssentialResource {
	res := pedestal.InitResource()

	// Initialize pointer fields with values from records
	if me.records.vcpuNum > 0 {
		vcpu := uint32(me.records.vcpuNum)
		res.Vcpu = &vcpu
	}

	if me.records.cpuWeight > 0 {
		weight := uint32(me.records.cpuWeight)
		res.CPUWeight = &weight
	} else {
		res.CPUWeight = nil
	}

	if me.records.cpuCapacity > 0 {
		capacity := uint32(me.records.cpuCapacity)
		res.CpuCpacity = &capacity
	}

	if me.records.memoryMB > 0 {
		memory := uint32(me.records.memoryMB)
		res.MemoryLimitMB = &memory
	} else {
		res.MemoryLimitMB = nil
	}

	// Set ClientCpuSet from cpuStr (convert byte array to string)
	res.ClientCpuSet = strings.TrimRight(string(me.records.cpuStr[:]), "\x00")

	return res
}

func (me *MicaExecutor) VcpuPin(cpuList []int) error {
	cpustr := pedestal.ParseCPUArr(cpuList)
	if cpustr == "" {
		return fmt.Errorf("received cpuList %v, parsed into an empty array", cpuList)
	}

	return me.UpdatePCPUConstrains(cpustr)
}

func (me *MicaExecutor) NeedUpdateCpuCap(target uint32) bool {
	current := uint32(0)
	if me.records.cpuCapacity > 0 {
		current = uint32(me.records.cpuCapacity)
	}
	if current == target && target >= uint32(cpuCapRatio)*pedestal.MaxCPUNum() {
		return false
	}
	return true
}

func (me *MicaExecutor) NeedUpdateMemLimit(target uint32) bool {
	return me.CurrentMemoryMB() != target
}

func (me *MicaExecutor) NeedUpdateVCpus(target uint32) bool {
	if target == 0 || target > pedestal.MaxCPUNum() {
		return false
	}
	current := uint32(0)
	if me.records.vcpuNum > 0 {
		current = uint32(me.records.vcpuNum)
	}
	return current != target
}

func (me *MicaExecutor) NeedUpdateCpuSet(_, _ string) bool {
	return true
}

func (me *MicaExecutor) NeedUpdateCpuShare(target uint32) bool {
	current := uint32(0)
	if me.records.cpuWeight > 0 {
		current = uint32(me.records.cpuWeight)
	}
	return current != target
}

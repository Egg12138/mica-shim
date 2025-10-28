package libmica

import (
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/pedestal"
	"strconv"
	"strings"
)

// TODO: this function is not in use now, we migrate from `UpdateVCPUs` to this function
// after allocating sandbox cpus in xen pool is finished.
func (me *MicaExecutor) UpdateSandboxPoolVCPUs() {}

// number of visible vcpus
func (me *MicaExecutor) UpdateVCPUNum(newVCPUs uint32) (oldCPUs, newCPUs uint32, retErr error)  {
	cmdArgs := []string{"VCPU", strconv.Itoa(int(newVCPUs))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to update vcpu number: %v", err)
		return uint32(me.records.vcpuNum), uint32(me.records.vcpuNum), err
	}
	return uint32(me.records.cpuWeight), newVCPUs, err
} 

// TODO: temporarily dirty-join string as command line, need to change to a better way
func (me *MicaExecutor) UpdatePCPUConstrains(cpus string) error {
	cmdArgs := []string{"CPU", cpus}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id,s)
	if err != nil {
		log.Warnf("failed to bind physical cpuset \"%s\" to container: %v", cpus,  err)
	} else {
		me.records.cpuStr = [MaxCPUStringLen]byte{}
		log.Info("updated to new cpuset")
	}
	return err
}


func (me *MicaExecutor) UpdateCPUCapacity(cap uint32) error {
	cmdArgs := []string{"CPUCpacity", strconv.Itoa(int(cap))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id,s)
	if err != nil {
		log.Warnf("failed to update cap time to %d that container can run: %v", cap,  err)
	} else {
		me.records.cpuCapacity = int(cap)
		log.Info("updated to new cpu capacity")
	}
	return err
}

func (me *MicaExecutor) UpdateCPUShare(weight uint32) error {
	cmdArgs := []string{"CPUWeight", strconv.Itoa(int(weight))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id,s)
	if err != nil {
		log.Warnf("failed to update cpu share time to %d that container can run: %v", weight,  err)
	} else {
		me.records.cpuWeight = int(weight)
		log.Info("updated to new cpu weight")
	}
	return err
}


func (me *MicaExecutor) UpdateMemoryLimit(memMiB uint32) error {
	cmdArgs := []string{"MaxMem", strconv.Itoa(int(memMiB))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id,s)
	if err != nil {
		log.Warnf("failed to request new max memory \"%d\" to container: %v", memMiB,  err)
	} else {
		me.records.memoryMB = int(memMiB)
		log.Debugf("update max memory to %d", memMiB)
	}
	return err
}

func (me *MicaExecutor) UpdateMemory(memMiB uint32) error {
	cmdArgs := []string{"Memory", strconv.Itoa(int(memMiB))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id,s)
	if err != nil {
		log.Warnf("failed to request new memory \"%d\" to container: %v", memMiB,  err)
	} else {
		me.records.memoryMB = int(memMiB)
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
	}

	if me.records.cpuCapacity > 0 {
		capacity := uint32(me.records.cpuCapacity)
		res.CpuCpacity = &capacity
	}

	if me.records.memoryMB > 0 {
		memory := uint32(me.records.memoryMB)
		res.MemoryLimitMB = &memory
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

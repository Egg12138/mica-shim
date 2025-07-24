package core

import (
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"strconv"
	"strings"
)

type RuntimeConfig struct {
	Debug bool
	SandboxCPUs uint32
	SandboxMemMB uint32
	
	// Global resource management settings
	MaxContainerCPUs   uint32 // Maximum CPU cores available for containers
	MaxContainerMemMB  uint32 // Maximum memory available for containers
	CPUSchedulerPolicy string // CPU scheduling policy: "round-robin", "priority", etc.
	MemoryOvercommit   bool   // Allow memory overcommit
	
	// MICA-specific configurations
	DefaultPedestal     string // Default pedestal type if not specified
	EnableResourceLimit bool   // Whether to enforce resource limits
}

func NewRuntimeSpec() *RuntimeConfig {
	spec := RuntimeConfig{
		Debug: false,
		SandboxCPUs: 0,
		SandboxMemMB: 0,
		
		// Defaults for global settings
		MaxContainerCPUs:   0,  // 0 means use system default
		MaxContainerMemMB:  0,  // 0 means use system default
		CPUSchedulerPolicy: "round-robin",
		MemoryOvercommit:   true,
		
		// MICA defaults
		DefaultPedestal:     "baremetal",
		EnableResourceLimit: true,
	}
	return &spec
}

func (r *RuntimeConfig) SetDebug(debug bool) *RuntimeConfig{
	log.Debugf("Setting debug to %v", debug)
	r.Debug = debug
	return r
}

func (r *RuntimeConfig) SetSandboxCPUs(cpu uint32) *RuntimeConfig{
	log.Debugf("Setting sandbox CPUs to %d", cpu)
	r.SandboxCPUs = cpu
	return r
}

func (r *RuntimeConfig) SetSandboxMemMB(mem uint32) *RuntimeConfig{
	log.Debugf("Setting sandbox memory to %dMB", mem)
	r.SandboxMemMB = mem
	return r
}

func (r *RuntimeConfig) SetMaxContainerCPUs(cpu uint32) *RuntimeConfig{
	log.Debugf("Setting max container CPUs to %d", cpu)
	r.MaxContainerCPUs = cpu
	return r
}

func (r *RuntimeConfig) SetMaxContainerMemMB(mem uint32) *RuntimeConfig{
	log.Debugf("Setting max container memory to %dMB", mem)
	r.MaxContainerMemMB = mem
	return r
}

func (r *RuntimeConfig) SetCPUSchedulerPolicy(policy string) *RuntimeConfig{
	log.Debugf("Setting CPU scheduler policy to %s", policy)
	r.CPUSchedulerPolicy = policy
	return r
}

func (r *RuntimeConfig) SetMemoryOvercommit(allow bool) *RuntimeConfig{
	log.Debugf("Setting memory overcommit to %v", allow)
	r.MemoryOvercommit = allow
	return r
}

func (r *RuntimeConfig) SetDefaultPedestal(pedestal string) *RuntimeConfig{
	log.Debugf("Setting default pedestal to %s", pedestal)
	r.DefaultPedestal = pedestal
	return r
}

func (r *RuntimeConfig) SetEnableResourceLimit(enable bool) *RuntimeConfig{
	log.Debugf("Setting enable resource limit to %v", enable)
	r.EnableResourceLimit = enable
	return r
}

// ParseRuntimeConfig parses runtime configuration from annotations
func ParseRuntimeConfig(annotations map[string]string) *RuntimeConfig {
	spec := NewRuntimeSpec()
	
	// Parse runtime-level annotations with mica annotation prefix
	for key, value := range annotations {
		if !strings.HasPrefix(key, defs.MicaAnnotationPrefix) {
			continue
		}
		
		// Remove prefix to get the config key
		configKey := strings.TrimPrefix(key, defs.MicaAnnotationPrefix+".")
		
		switch configKey {
		case "runtime.debug":
			if debug, err := strconv.ParseBool(value); err == nil {
				spec.SetDebug(debug)
			}
		case "runtime.sandbox.cpus":
			if cpus, err := strconv.ParseUint(value, 10, 32); err == nil {
				spec.SetSandboxCPUs(uint32(cpus))
			}
		case "runtime.sandbox.memory":
			if mem, err := strconv.ParseUint(value, 10, 32); err == nil {
				spec.SetSandboxMemMB(uint32(mem))
			}
		case "runtime.max_container_cpus":
			if cpus, err := strconv.ParseUint(value, 10, 32); err == nil {
				spec.SetMaxContainerCPUs(uint32(cpus))
			}
		case "runtime.max_container_memory":
			if mem, err := strconv.ParseUint(value, 10, 32); err == nil {
				spec.SetMaxContainerMemMB(uint32(mem))
			}
		case "runtime.cpu_scheduler_policy":
			spec.SetCPUSchedulerPolicy(value)
		case "runtime.memory_overcommit":
			if overcommit, err := strconv.ParseBool(value); err == nil {
				spec.SetMemoryOvercommit(overcommit)
			}
		case "runtime.default_pedestal":
			spec.SetDefaultPedestal(value)
		case "runtime.enable_resource_limit":
			if enable, err := strconv.ParseBool(value); err == nil {
				spec.SetEnableResourceLimit(enable)
			}
		}
	}
	
	return spec
}
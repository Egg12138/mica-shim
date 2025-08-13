package oci

import (
	"encoding/json"
	defs "mica-shim/definitions"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type RuntimeConfig struct {
	Debug        bool
	SandboxCPUs  uint32
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
		Debug:        false,
		SandboxCPUs:  0,
		SandboxMemMB: 0,

		// Defaults for global settings
		MaxContainerCPUs:   0, // 0 means use system default
		MaxContainerMemMB:  0, // 0 means use system default
		CPUSchedulerPolicy: "round-robin",
		MemoryOvercommit:   true,

		// MICA defaults
		DefaultPedestal:     "baremetal",
		EnableResourceLimit: true,
	}
	return &spec
}

func (r *RuntimeConfig) SetDebug(debug bool) *RuntimeConfig {
	r.Debug = debug
	return r
}

func (r *RuntimeConfig) SetSandboxCPUs(cpu uint32) *RuntimeConfig {
	r.SandboxCPUs = cpu
	return r
}

func (r *RuntimeConfig) SetSandboxMemMB(mem uint32) *RuntimeConfig {
	r.SandboxMemMB = mem
	return r
}

func (r *RuntimeConfig) SetMaxContainerCPUs(cpu uint32) *RuntimeConfig {
	r.MaxContainerCPUs = cpu
	return r
}

func (r *RuntimeConfig) SetMaxContainerMemMB(mem uint32) *RuntimeConfig {
	r.MaxContainerMemMB = mem
	return r
}

func (r *RuntimeConfig) SetCPUSchedulerPolicy(policy string) *RuntimeConfig {
	r.CPUSchedulerPolicy = policy
	return r
}

func (r *RuntimeConfig) SetMemoryOvercommit(allow bool) *RuntimeConfig {
	r.MemoryOvercommit = allow
	return r
}

func (r *RuntimeConfig) SetDefaultPedestal(pedestal string) *RuntimeConfig {
	r.DefaultPedestal = pedestal
	return r
}

func (r *RuntimeConfig) SetEnableResourceLimit(enable bool) *RuntimeConfig {
	r.EnableResourceLimit = enable
	return r
}

// ParseRuntimeConfig parses runtime configuration from annotations
func ParseRuntimeConfig(annotations map[string]string) *RuntimeConfig {
	spec := NewRuntimeSpec()

	// Parse runtime-level annotations with mica annotation prefix
	for key, value := range annotations {
		if !strings.HasPrefix(key, defs.MicraAnnotationPrefix) {
			continue
		}

		// Remove prefix to get the config key
		configKey := strings.TrimPrefix(key, defs.MicraAnnotationPrefix+".")

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

func ParseConfigJSON(bundle string) (specs.Spec, error) {
	// For docker , config.v2.json, this line is useless;
	configPath := filepath.Join(bundle, "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return specs.Spec{}, err
	}

	var config specs.Spec
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return specs.Spec{}, err
	}

	return config, nil
}

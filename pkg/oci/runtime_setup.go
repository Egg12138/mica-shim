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
	Pedestal            string // Default pedestal type if not specified
	EnableResourceLimit bool   // Whether to enforce resource limits
	PauseImage          string
}

// return a initialized RuntimeConfig
func NewRuntimeConfig() *RuntimeConfig {
	spec := RuntimeConfig{
		// MICA defaults
		Pedestal:            "baremetal",
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
	r.Pedestal = pedestal
	return r
}

func (r *RuntimeConfig) SetEnableResourceLimit(enable bool) *RuntimeConfig {
	r.EnableResourceLimit = enable
	return r
}

// ParseRuntimeConfig parses runtime configuration from annotations
// TODO: match these dummy config items with actual implementation, define prefix in package definitions
func (cfg *RuntimeConfig) ParseRuntimeConfig(annotations map[string]string) *RuntimeConfig {
	cfg.PauseImage = defs.PauseImage
	// Parse runtime-level annotations with mica annotation prefix
	for key, value := range annotations {
		if !strings.HasPrefix(key, defs.MicraAnnotationPrefix) || value == "" {
			continue
		}

		// Remove prefix to get the config key

		switch key {
		case defs.RuntimeDebug:
			if debug, err := strconv.ParseBool(value); err == nil {
				cfg.SetDebug(debug)
			}
		case "runtime.sandbox.cpus":
			if cpus, err := strconv.ParseUint(value, 10, 32); err == nil {
				cfg.SetSandboxCPUs(uint32(cpus))
			}
		case "runtime.sandbox.memory":
			if mem, err := strconv.ParseUint(value, 10, 32); err == nil {
				cfg.SetSandboxMemMB(uint32(mem))
			}
		case "runtime.max_container_cpus":
			if cpus, err := strconv.ParseUint(value, 10, 32); err == nil {
				cfg.SetMaxContainerCPUs(uint32(cpus))
			}
		case "runtime.max_container_memory":
			if mem, err := strconv.ParseUint(value, 10, 32); err == nil {
				cfg.SetMaxContainerMemMB(uint32(mem))
			}
		case "runtime.cpu_scheduler_policy":
			cfg.SetCPUSchedulerPolicy(value)
		case "runtime.memory_overcommit":
			if overcommit, err := strconv.ParseBool(value); err == nil {
				cfg.SetMemoryOvercommit(overcommit)
			}
		case defs.Pedtype:
			cfg.SetDefaultPedestal(value)
		case "runtime.enable_resource_limit":
			if enable, err := strconv.ParseBool(value); err == nil {
				cfg.SetEnableResourceLimit(enable)
			}
		case "runtime.pause":
			cfg.PauseImage = value
		}
	}

	return cfg
}

func parseConfigJSON(file string) (specs.Spec, error) {
	configBytes, err := os.ReadFile(file)
	if err != nil {
		return specs.Spec{}, err
	}

	var config specs.Spec
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return specs.Spec{}, err
	}

	return config, nil
}

func LoadSpec(bundle string) (specs.Spec, error) {
	// For docker , config.v2.json, this line is useless;
	configPath := filepath.Join(bundle, "config.json")
	return parseConfigJSON(configPath)
}

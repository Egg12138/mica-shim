package oci

import (
	"encoding/json"
	defs "mica-shim/definitions"
	"mica-shim/pkg/fileutils"
	"mica-shim/pkg/pedestal"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gookit/ini/v2"
	"github.com/opencontainers/runtime-spec/specs-go"
)

var (
	runtimeConfigKeys = []string{
		defs.ConfigKeyDebug,
		defs.ConfigStaticResource,
		defs.ConfigKeyStateDir,
		defs.ConfigKeyLinuxContainer,
		defs.ConfigKeyClientLimit,
		defs.ConfigKeyMaxContainerVCPU,
	}
	
)

type RuntimeConfig struct {
	Debug        bool
	SandboxCPUs  uint32
	SandboxMemMB uint32

	// Global resource management settings
	MaxContainerCPUs   uint32 // Maximum CPU cores available for containers
	MaxContainerMemMB  uint32 // Maximum memory available for containers
	CPUSchedulerPolicy string 
	MemoryOvercommit   bool   

	// MICA-specific configurations
	Pedestal            string // Default pedestal type if not specified
	PauseImage          string
	StaticResourceManagement bool
}

// return a default RuntimeConfig
func NewRuntimeConfig() *RuntimeConfig {
	ped := pedestal.GetHostPed()
	var staticResource bool
	if ped == pedestal.OpenAMP {
		staticResource = true
	}

	spec := RuntimeConfig{
		// MICA defaults
		Pedestal:            pedestal.GetHostPed().String(),
		StaticResourceManagement: staticResource,
	}
	return &spec
}


// ini conf
// TODO: with expanding of micran runtime config, we will migrate gookit.ini/v2 to 
// out ParseConfigINI, ParseConfigINI requires only half memory of ini package and faster
// for large ini file parsing
func (r *RuntimeConfig) ParseRuntimeFromFile(configPath string) error {
	fileutils.ParseConfigINI(configPath, runtimeConfigKeys)
	err := ini.LoadExists(configPath)
	if err != nil {
		return err
	}

	return nil
}

func (r *RuntimeConfig) convertRawConfig(raw map[string]string) {

}

func filterRuntimeItems() bool {return true}

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


// ParseRuntimeConfig parses runtime configuration from annotations
// TODO: match these dummy config items with actual implementation, define prefix in package definitions
func (cfg *RuntimeConfig) ParseRuntimeConfig(annotations map[string]string) *RuntimeConfig {
	cfg.PauseImage = defs.PauseImage
	// Parse runtime-level annotations with mica annotation prefix
	for key, value := range annotations {
		if !strings.HasPrefix(key, defs.MicraAnnotationPrefix) || value == "" {
			continue
		}

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

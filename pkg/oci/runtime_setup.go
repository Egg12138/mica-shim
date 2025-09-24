package oci

import (
	"encoding/json"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/pedestal"
	"mica-shim/pkg/utils"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// configurations keys
const (
	// default=true
	KeyStaticResource = "static_resource"
	// default=0, unlimited
	KeyClientLimit = "max_client_number"
	// default=false
	KeyLinuxContainer = "enable_host_container"
	KeyDebug = "debug"
	// default=defs.StateDir
	KeyStateDir = "state_dir"
	// default=defs.PauseImage
	KeyPauseImg = "pause_image"
	// default=0, unlimited
	KeyMaxContainerVCPU = "max_container_vcpu"
	// default=1
	KeySandboxMinVCPU = "sandbox_minimum_vcpu"
	// only for Xen; default=false
	KeyHugePage = "hugepage_enable"
	// default base memory for container
	KeyMinMemory = "container_minmem"
	KeyMaxMemory = "container_maxmem"
)



var (
	thredsholdMemHigh = pedestal.MemHighThreshold()
	thredsholdMemLow = pedestal.MemLowThreshold()
	runtimeConfigKeys = []string{
		KeyStaticResource,
		KeyClientLimit,
		KeyDebug,
		KeyLinuxContainer,
		KeyStateDir,
		KeyPauseImg,
		KeyMaxContainerVCPU,
		KeySandboxMinVCPU,
		KeyHugePage,
		KeyMaxMemory,
		KeyMinMemory,
	}
	
)

type RuntimeConfig struct {
	Debug        bool
	SandboxCPUs  uint32
	SandboxMemMB uint32
	// TODO: enable Linux host act as a container
	HostLinuxContainer bool
	MaxClinetNum uint32

	// Global resource management settings
	MaxContainerCPUs   uint32 // Maximum CPU cores visible for containers
	MaxContainerMemMB  uint32 // Maximum memory available for containers
	MinContainerMemMB          uint32 // Minimum memory for containers
	HugePageSupport      bool
	StaticResourceManagement bool

	// MICA-specific configurations
	ImagePath   string
	AuxFilePath string

	PauseImage          string
	MiniVCPUNum uint32
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
		StaticResourceManagement: staticResource,
	}
	return &spec
}


// ini conf
// TODO: with expanding of micran runtime config, we will migrate gookit.ini/v2 to 
// out ParseConfigINI, ParseConfigINI requires only half memory of ini package and faster
// for large ini file parsing
func (r *RuntimeConfig) ParseRuntimeFromFile(configPath string) error {
	filtered, err := utils.ParseConfigINI(configPath, runtimeConfigKeys)
	if err != nil {
		return err
	}

	log.Debugf("parsed runtime config: %v", filtered)
	r.convertRawConfig(filtered)
	return nil
}

func (r *RuntimeConfig) convertRawConfig(raw map[string]string) {
	r.SetStaticResourceManagement(raw[KeyStaticResource])
	r.SetDebug(raw[KeyDebug])
	r.SetPauseImage(raw[KeyPauseImg])
	r.SetMaxContainerCPUs(raw[KeyMaxContainerVCPU])
	r.SetMaxContainerMemMB(raw[KeyMaxMemory])
	r.SetMinContainerMemMB(raw[KeyMinMemory])
	r.SetMiniVCPUNum(raw[KeySandboxMinVCPU])
	r.SetHugePageSupport(raw[KeyHugePage])
	r.SetStateDir(raw[KeyStateDir])
}


func (r *RuntimeConfig) SetDebug(debugStr string) {
	debug, err := strconv.ParseBool(debugStr)
	if err != nil {
		log.Debugf("Failed to parse debug %v into bool", debugStr, err)
		debug = false
	}
	r.Debug = debug
}

func (r *RuntimeConfig) SetSandboxCPUs(cpuString string) {
	cpu, err := strconv.ParseUint(cpuString, 10, 32)
	if err != nil {
		log.Debugf("Failed to parse sandbox cpus %v into uint32", cpuString, err)
	}
	r.SandboxCPUs = uint32(cpu)
}

func (r *RuntimeConfig) SetSandboxMemMB(memString string) {
	mem, err := strconv.ParseUint(memString, 10, 32)
	if err != nil {
		log.Debugf("Failed to parse sandbox memory %v into uint32", memString, err)
	}
	r.SandboxMemMB = uint32(mem)
}

func (r *RuntimeConfig) SetMaxContainerCPUs(cpuString string) {
	cpu, err := strconv.ParseUint(cpuString, 10, 32)
	if err != nil {
		log.Debugf("Failed to parse max container cpus %v into uint32", cpuString, err)
	}
	r.MaxContainerCPUs = uint32(cpu)
}

func (r *RuntimeConfig) SetMaxContainerMemMB(memString string) {
	mem, err := strconv.ParseUint(memString, 10, 32)
	if err != nil || memoryOutOfRange(uint32(mem)){
		log.Warnf("Failed to parse max container memory %v into uint32 or out or range: %v", memString, err)
		r.MaxContainerMemMB = thredsholdMemHigh
		return
	}
	
	r.MaxContainerMemMB = uint32(mem)
}

func (r *RuntimeConfig) SetMinContainerMemMB(memString string) {
	mem, err := strconv.ParseUint(memString, 10, 32)
	if err != nil || memoryOutOfRange(uint32(mem)){
		log.Debugf("Failed to parse min container memory %v into uint32 or out or range", memString, err)
		r.MinContainerMemMB = thredsholdMemLow
		return
	}

	r.MinContainerMemMB = uint32(mem)
}



func (r *RuntimeConfig) SetHugePageSupport(hugePageStr string) {
	hugePage, err := strconv.ParseBool(hugePageStr)
	if err != nil {
		log.Debugf("Failed to parse hugepage %v into bool", hugePageStr, err)
		hugePage = false
	}
	r.HugePageSupport = hugePage
}

func (r *RuntimeConfig) SetPauseImage(pauseImage string) {
	r.PauseImage = pauseImage
}

func (r *RuntimeConfig) SetStaticResourceManagement(staticResourceStr string) {
	staticResource, err := strconv.ParseBool(staticResourceStr)
	if err != nil {
		log.Debugf("Failed to parse static_resource %v into bool", staticResourceStr, err)
		staticResource = false
	}
	r.StaticResourceManagement = staticResource
}

func (r *RuntimeConfig) SetMiniVCPUNum(miniVCPUString string) {
	miniVCPU, err := strconv.ParseUint(miniVCPUString, 10, 32)
	if err != nil {
		log.Debugf("Failed to parse mini vcpu %v into uint32", miniVCPUString, err)
	}
	r.MiniVCPUNum = uint32(miniVCPU)
}

func (r *RuntimeConfig) SetClientLimit(clientLimitString string) {
	clientLimit, err := strconv.ParseUint(clientLimitString, 10, 32)
	if err != nil {
		log.Debugf("Failed to parse client limit %v into uint32", clientLimitString, err)
	}
	r.MaxClinetNum = uint32(clientLimit)
}

func (r *RuntimeConfig) SetLinuxContainer(linuxContainerStr string) {
	linuxContainer, err := strconv.ParseBool(linuxContainerStr)
	if err != nil {
		log.Debugf("Failed to parse linux container %v into bool", linuxContainerStr, err)
		linuxContainer = false
	}
	r.HostLinuxContainer = linuxContainer
}

func (r *RuntimeConfig) SetStateDir(stateDir string) {
	// Note: This field doesn't exist in RuntimeConfig yet, but the key is defined
	// For now, we'll just log it since it's a path configuration
	log.Debugf("Setting state dir to: %v", stateDir)
}


// ParseRuntimeConfigFromAnno parses runtime configuration from annotations
// Annotations holds highest priority for values
// TODO: match these dummy config items with actual implementation, define prefix in package definitions
func (cfg *RuntimeConfig) ParseRuntimeConfigFromAnno(annotations map[string]string) *RuntimeConfig {
	cfg.PauseImage = defs.PauseImage
	// Parse runtime-level annotations with mica annotation prefix
	for key, value := range annotations {
		if !strings.HasPrefix(key, defs.MicraAnnotationPrefix) || value == "" {
			continue
		}

		switch key {
		case defs.RuntimeDebug:
			cfg.SetDebug(value)
		case "runtime.sandbox.cpus":
			cfg.SetSandboxCPUs(value)
		case "runtime.sandbox.memory":
			cfg.SetSandboxMemMB(value)
		case "runtime.max_container_cpus":
			cfg.SetMaxContainerCPUs(value)
		case "runtime.max_container_memory":
			cfg.SetMaxContainerMemMB(value)
		case "runtime.cpu_scheduler_policy":
			log.Debugf("CPU scheduler policy not implemented, ignoring: %s", value)
		case "runtime.memory_overcommit":
			log.Debugf("Memory overcommit not implemented, ignoring: %s", value)
		case "runtime.pause":
			cfg.SetPauseImage(value)
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

// 2MB < cfgmem < 
func memoryOutOfRange(cfgmem uint32) bool {
	if cfgmem > thredsholdMemHigh {
		log.Debugf("configurated micran memory out of range, set to %dMB by default", thredsholdMemHigh)
		return true
	}

	if cfgmem < thredsholdMemLow {
		log.Debugf("configurated micran memory out of range, set to %dMB by default", thredsholdMemLow)
		return true
	}

	return false

}

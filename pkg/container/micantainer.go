package cntr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/libmica"
	oci "mica-shim/pkg/oci"

	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/mount"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// alternative: docker, containerd, isulad
// high level container engine
var (
	HighLevelCE    = "containerd"
	RequiredLabels = []string{
		defs.Firmware,
		defs.OS,
	}
)

const prefix = defs.MicaLabelPrefix

// Structures:
// - OCISpec: specs.Spec in config.json (or config.v2.json)
// - ContainerConfig: All configurations parsed from bundle (replaces old ContainerConf and MicaContainerInfo)
// - libmica.micaClientConf
// - Container

// ocispec => ContainerConfig => Container => libmica.micaClientConf

// *************** ocispec *************** //
// assume containerd, parse config.json from bundle
// TODO: iSulad
func parseConfigJSON(bundle string) (specs.Spec, error) {
	// For docker higher version , config.v2.json
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

// *************** INI mica configs *************** //

// stripQuotes removes surrounding quotes from a string if both start and end quotes match
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// a faster ini parsing method, by reading line by line
func parseConfigINI(bundle string) (map[string]string, error) {
	configPath := filepath.Join(bundle, "rootfs", defs.DefaultClientConf)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// If config file doesn't exist, return empty map (not an error)
		log.Debugf("No %s found under bundle %s", defs.DefaultClientConf, bundle)
		return make(map[string]string), nil
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mica config file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inMicaSection := false

	// Pre-allocate map for faster lookups
	parsedFields := make(map[string]string, 8)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if len(line) == 0 || line[0] == '#' || line[0] == ';' {
			continue
		}

		if line[0] == '[' && line[len(line)-1] == ']' {
			sectionName := strings.ToLower(line[1 : len(line)-1])
			inMicaSection = inList(defs.OKSectionList[:], sectionName)
			continue
		}

		if !inMicaSection {
			continue
		}

		// Find the separator (= or :)
		sepIndex := strings.IndexByte(line, '=')
		if sepIndex == -1 {
			sepIndex = strings.IndexByte(line, ':')
		}
		if sepIndex == -1 {
			continue // Skip malformed lines
		}

		key := strings.ToLower(strings.TrimSpace(line[:sepIndex]))
		value := strings.TrimSpace(line[sepIndex+1:])

		// Remove surrounding quotes if present
		value = stripQuotes(value)

		parsedFields[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading mica config file: %v", err)
	}

	log.Debugf("Parsed MICA config from %s: %+v", configPath, parsedFields)
	return parsedFields, nil
}

// ContainerConfig represents all configurations parsed from bundle
// This includes OCI spec, MICA-specific configurations, and runtime state
type ContainerConfig struct {
	// OCI Specification
	Spec specs.Spec

	// Bundle information
	Bundle string
	Type   ContainerType
	Detach bool

	// BUG: overlapped fields: remove MicaConf, leaving extracted MicaLabels and extraLabels
	// Parsed configuration values
	// Firmware and pedestal
	// MICA-specific configurations from client.conf
	extraLabels  map[string]string
	relativePath string // relative firmware path to the bundle
	pedestalType PedType
	pedestalConf string
	os           string
	ncpu         int // requested CPU count (default = 1)

	cpuLimit   int    // CPU limit from OCI spec
	cpusetCpus string // cpuset.cpus specification
	cpuShares  uint64 // CPU shares (relative weight)
	cpuQuota   int64  // CPU quota in microseconds
	cpuPeriod  uint64 // CPU period in microseconds

	// Memory resource limits from OCI spec
	memoryLimit       int64   // Memory limit in bytes
	memoryReservation int64   // Memory soft limit in bytes
	memorySwap        int64   // Memory + swap limit in bytes
	memoryKernel      int64   // Kernel memory limit in bytes
	memorySwappiness  *uint64 // Memory swappiness (0-100)
	oomKillDisable    bool    // Whether to disable OOM killer

	// Runtime state
	cpu int // allocated CPU (-1 if not allocated)

	mu sync.RWMutex
}

type PedType int

const (
	// 0
	Baremetal PedType = iota
	// 1
	Jailhouse
	// 2
	Xen
	Unknown
)

// String returns the string representation of PedType
func (p PedType) String() string {
	switch p {
	case Baremetal:
		return "baremetal"
	case Jailhouse:
		return "jailhouse"
	case Xen:
		return "xen"
	default:
		return "unknown"
	}
}

func ParsePedType(s string) PedType {
	switch strings.ToLower(s) {
	case "baremetal", "openamp", "":
		return Baremetal
	case "jailhouse", "jail":
		return Jailhouse
	case "xen":
		return Xen
	default:
		return Unknown // default to baremetal
	}
}

type Pedestal struct {
	PedestalType PedType `json:"pedestal_type"`
	PedestalConf string  `json:"pedestal_conf"`
}

type ContainerType string

const (
	PodContainer ContainerType = "pod"
	// pod sandbox
	PodSandbox ContainerType = "sandbox"
	SideCar    ContainerType = "sidecar"
	// a regular containers created not by CRI
	Regular      ContainerType = "regular"
	UnknownCtype ContainerType = "unknown"
)

func (ct ContainerType) IsRegularContainer() bool {
	return ct == Regular
}

func (ct ContainerType) CanBeSandbox() bool {
	return ct == Regular || ct == SideCar
}

type Container struct {
	bundle   string
	exitTime time.Time
	ID       string
	io       *libmica.MicaIO
	exitCode uint32
	// int32: RUNNING, STOPPED, PAUSED, PAUSING, CREATED, UNKNOWN...
	status task.Status
	cType  ContainerType
	config *ContainerConfig // single source of configuration truth
}

// *************** Constructors *************** //

func loadSpec(bundle string) (specs.Spec, error) {
	ociSpec, err := parseConfigJSON(bundle)
	if err != nil {
		return specs.Spec{}, err
	}
	return ociSpec, nil
}

// The core container configuration parser
// This function parses all configurations from the bundle and returns a complete ContainerConfig
func parseContainerConfig(bundle string, ocispec specs.Spec, cType ContainerType, detach bool) (*ContainerConfig, error) {
	// Parse MICA configuration from client.conf
	micaConf, err := parseConfigINI(bundle)
	if err != nil {
		return nil, err
	}

	config := &ContainerConfig{
		// OCI and bundle info
		Spec:   ocispec,
		Bundle: bundle,
		Type:   cType,
		Detach: detach,

		// MICA configuration
		extraLabels: make(map[string]string),

		// Initialize with defaults
		relativePath: "",
		pedestalType: Unknown,
		pedestalConf: "",
		os:           "",
		ncpu:         1,
		cpuLimit:     0,
		cpusetCpus:   "",
		cpuShares:    0,
		cpuQuota:     0,
		cpuPeriod:    0,

		// Memory defaults
		memoryLimit:       0,
		memoryReservation: 0,
		memorySwap:        0,
		memoryKernel:      0,
		memorySwappiness:  nil,
		oomKillDisable:    false,

		cpu: -1, // not allocated yet

		mu: sync.RWMutex{},
	}

	// Parse MICA-specific labels
	if err := config.parseMicaLabels(micaConf); err != nil {
		log.Debugf("failed to parse mica labels: %v", err)
		return nil, err
	}

	// Parse CPU resources from OCI spec
	if err := config.parseOCICPUResources(&ocispec); err != nil {
		log.Debugf("failed to parse OCI CPU resources: %v", err)
		return nil, err
	}

	// Parse Memory resources from OCI spec
	if err := config.parseOCIMemoryResources(&ocispec); err != nil {
		log.Debugf("failed to parse OCI Memory resources: %v", err)
		return nil, err
	}

	// Validate resource limits against system constraints
	if err := validateResourceLimits(config); err != nil {
		log.Warnf("Resource validation warning: %v", err)
		// Don't fail the container creation for resource validation warnings
		// but log them for visibility
	}

	// Set default OS if not specified
	if config.os == "" {
		log.Warn("os is not set, default to zephyr")
		config.os = "zephyr"
	}

	log.Infof("Container resource limits - CPU: %s, Memory: %s",
		formatCPULimit(config), formatMemoryLimit(config))
	return config, nil
}

// deprecated: containerInfoParse is now replaced by parseContainerConfig
func (conf *ContainerConfig) containerInfoParse() (*ContainerConfig, error) {
	log.Warn("containerInfoParse is deprecated, use parseContainerConfig instead")
	return conf, nil
}

func getContainerType(spec *specs.Spec) (ContainerType, error) {
	for _, key := range CRIContainerTypeKeyList {
		containerType, ok := spec.Annotations[key]
		if !ok {
			continue
		}

		for _, t := range CRIContainerTypeList {
			if t.annotation == containerType {
				return t.containerType, nil
			}
		}
		return UnknownCtype, fmt.Errorf("unknown container type: %s", containerType)
	}
	return Regular, nil
}

type Mount struct {
	Type    string
	Source  string
	Target  string
	Options []string
}

func NewContainer(id, bundle string, rootfs []*types.Mount, terminal bool) (_ *Container, retErr error) {
	detach := !terminal
	spec, err := loadSpec(bundle)
	rtConfig, err := getRuntimeConfig(&spec)
	if err != nil {
		return nil, fmt.Errorf("failed to load runtime config: %w", err)
	}
	log.Pretty("runtime config: %v", rtConfig)
	enableTTy := spec.Process.Terminal
	// when tty is disable, stdio use regular pipe, which containerd needs pipe io to log
	disableOutput := detach && enableTTy
	// TALK: act like kata?
	// runtimeConfig, err := parseRuntimeConfig(r, spec.Annotations)

	if err != nil {
		return nil, fmt.Errorf("failed to load container spec: %w", err)
	}
	ctype, err := getContainerType(&spec)
	if err != nil {
		return nil, fmt.Errorf("failed to get container type: %w", err)
	}

	switch ctype {
	case PodSandbox, Regular:

		if ctype == PodSandbox {
			log.Info("TODO: setup cpu/mem resources size for sandbox")
		} else {
			log.Info("Only set one one cpu for a container. Memory is not limited")
		}

		var mounts []mount.Mount

		for _, mnt := range rootfs {
			mounts = append(mounts, mount.Mount{
				Type:    mnt.Type,
				Source:  mnt.Source,
				Target:  mnt.Target,
				Options: mnt.Options,
			})

			// NOTICE: Currently support internal rootfs only!
			rootfsPath := filepath.Join(bundle, "rootfs")
			if len(mounts) > 0 {
				if err := os.Mkdir(rootfsPath, 0711); err != nil && !os.IsExist(err) {
					return nil, err
				}
			}

			if err := mount.All(mounts, rootfsPath); err != nil {
				return nil, fmt.Errorf("failed to mount rootfs: %w", err)
			}
			defer func() {
				if retErr != nil {
					if err := mount.UnmountMounts(mounts, rootfsPath, 0); err != nil {
						log.Errorf("failed to unmount rootfs: %v", err)
					}
				}
			}()

		}

	default:
		log.Fatalf("container type: %s is not supported yet", ctype)
	}

	// preset for sandbox, pod...
	presetSandbox()

	bundlePath, err := validBundleRootfs(id, bundle)
	log.Debugf("bundle content: %s", walkDir(bundlePath))
	if err != nil {
		return nil, err
	}

	config, err := parseContainerConfig(bundlePath, spec, ctype, disableOutput)
	if err != nil {
		return nil, fmt.Errorf("failed to parse container config: %w", err)
	}

	container, err := newContainer(id, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create mica container instance: %w", err)
	}
	return container, err
}

// A new container instance, initialized with complete configuration
// func newContainer(r *taskAPI.CreateTaskRequest, config *ContainerConfig) (*Container, error) {
func newContainer(id string, config *ContainerConfig) (*Container, error) {
	container := &Container{
		bundle:   config.Bundle,
		ID:       id,
		io:       nil,
		exitCode: 0,
		cType:    config.Type,
		config:   config,
		// status: remain empty
	}
	log.Debugf("new container: %+v", container)

	if !container.validMicaContainer() {
		return nil, fmt.Errorf("invalid mica container: %+v", container)
	}

	return container, nil
}

// Do not handle unmatched labels here
func (r *ContainerConfig) parseMicaLabels(labels map[string]string) error {
	// TODO: make sure we do can find the firmware path in container bundle
	// Parse firmware path
	// preserved os:
	// "zephyr", "uniproton", "linux"

	for k, v := range labels {
		switch k {
		case defs.Firmware:
			r.relativePath = filepath.Join("rootfs", v)
		case defs.Pedestal:
			r.pedestalType = ParsePedType(v)
		case defs.PedestalConf:
			r.pedestalConf = v
		case defs.OS:
			if v == "" {
				return fmt.Errorf("missing os label")
			}
			log.Debugf("current os label: %s", v)
			if !validOS(v) {
				return fmt.Errorf("invalid os label: %s", v)
			}
			r.os = v
		case defs.Ncpu:
			r.ncpu = getNcpu(v)
		default:
			if strings.HasPrefix(k, prefix) {
				r.extraLabels[k] = v
			}
		}
	}
	log.Debugf(`parsed mica client conf: 
		relativePath = %s,
		pedestalType = %s,
		pedestalConf = %s,
		os = %s,
		ncpu = %d,
		:: extra mappings = %v
	`, r.relativePath, r.pedestalType, r.pedestalConf, r.os, r.ncpu, r.extraLabels)
	return nil
}

// parseOCICPUResources parses CPU resource limits from OCI spec
func (r *ContainerConfig) parseOCICPUResources(spec *specs.Spec) error {
	if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.CPU == nil {
		log.Debugf("No CPU resources specified in OCI spec")
		return nil
	}

	cpu := spec.Linux.Resources.CPU

	// Parse CPU quota and period to get CPU limit
	if cpu.Quota != nil && cpu.Period != nil && *cpu.Period > 0 {
		r.cpuQuota = *cpu.Quota
		r.cpuPeriod = *cpu.Period
		cpuLimit := int(*cpu.Quota / int64(*cpu.Period))
		if cpuLimit > 0 {
			r.cpuLimit = cpuLimit
			log.Debugf("Parsed CPU limit from quota/period: %d (quota: %d, period: %d)", cpuLimit, *cpu.Quota, *cpu.Period)
		}
	}

	if cpu.Shares != nil {
		r.cpuShares = *cpu.Shares
		log.Debugf("Parsed CPU shares: %d", *cpu.Shares)
	}

	if cpu.Cpus != "" {
		r.cpusetCpus = cpu.Cpus
		log.Debugf("Parsed cpuset.cpus: %s", r.cpusetCpus)
	}

	// Parse realtime CPU constraints if present
	if cpu.RealtimeRuntime != nil {
		log.Debugf("Realtime CPU runtime specified: %d", *cpu.RealtimeRuntime)
	}

	return nil
}

// parseOCIMemoryResources parses Memory resource limits from OCI spec
func (r *ContainerConfig) parseOCIMemoryResources(spec *specs.Spec) error {
	if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.Memory == nil {
		log.Debugf("No Memory resources specified in OCI spec")
		return nil
	}

	memory := spec.Linux.Resources.Memory

	// Parse memory limit
	if memory.Limit != nil {
		r.memoryLimit = *memory.Limit
		log.Debugf("Parsed memory limit: %d bytes", *memory.Limit)
	}

	// Parse memory reservation (soft limit)
	if memory.Reservation != nil {
		r.memoryReservation = *memory.Reservation
		log.Debugf("Parsed memory reservation: %d bytes", *memory.Reservation)
	}

	// Parse memory + swap limit
	if memory.Swap != nil {
		r.memorySwap = *memory.Swap
		log.Debugf("Parsed memory swap limit: %d bytes", *memory.Swap)
	}

	// Parse kernel memory limit
	if memory.Kernel != nil {
		r.memoryKernel = *memory.Kernel
		log.Debugf("Parsed kernel memory limit: %d bytes", *memory.Kernel)
	}

	// Parse memory swappiness
	if memory.Swappiness != nil {
		swappiness := uint64(*memory.Swappiness)
		r.memorySwappiness = &swappiness
		log.Debugf("Parsed memory swappiness: %d", *memory.Swappiness)
	}

	// Parse OOM killer disable flag
	if memory.DisableOOMKiller != nil {
		r.oomKillDisable = *memory.DisableOOMKiller
		log.Debugf("Parsed OOM killer disable: %v", *memory.DisableOOMKiller)
	}

	return nil
}

// parseDockerConfigJSON tries to parse bundle information from the bundle directory
// For docker, we can get the config.v2.json
// But for containerd, we need to use the containerd API to fetch metadata stored in bolt db
func parseDockerConfigJSON(bundle string) (*ContainerConfig, error) {
	dockerConfigPath := filepath.Join(bundle, "config.v2.json")
	if _, err := os.Stat(dockerConfigPath); err == nil {
		dockerConfigData, err := os.ReadFile(dockerConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config.v2.json: %v", err)
		}
		var containerConfigs ContainerConfig
		if err := json.Unmarshal(dockerConfigData, &containerConfigs); err != nil {
			return nil, fmt.Errorf("failed to parse config.v2.json: %v", err)
		}
		return &containerConfigs, nil
	}
	return nil, nil
}

func parseiSuladContainerConfig(bundle string) (*ContainerConfig, error) {
	// TODO: parse isulad container config
	return nil, nil
}

func (r *ContainerConfig) FirmwarePath() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	log.Debugf("relative FirmwarePath: %s", r.relativePath)
	return r.relativePath
}

// Pedestal returns the pedestal information
func (r *ContainerConfig) Ped() *Pedestal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return &Pedestal{
		PedestalType: r.pedestalType,
		PedestalConf: r.pedestalConf,
	}
}

// PedestalType returns the pedestal type
func (r *ContainerConfig) PedestalType() PedType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pedestalType
}

// PedestalConf returns the pedestal configuration
func (r *ContainerConfig) PedestalConf() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pedestalConf
}

func (r *ContainerConfig) OS() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.os
}

// GetCompatibility returns compatibility information for a specific component
func (r *ContainerConfig) Compatibility(component string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.extraLabels[defs.Compat]
}

func (r *ContainerConfig) GetAllLabelsRef() *map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	labels := &r.extraLabels
	return labels
}

// cpuUnset is alway callee, hence lock is not needed
func (r *ContainerConfig) cpuUnset() bool {
	return r.cpu == -1
}

// CPULimit returns the effective CPU limit for this container
func (r *ContainerConfig) CPULimit() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cpuLimit
}

// CpusetCpus returns the cpuset.cpus specification
func (r *ContainerConfig) CpusetCpus() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cpusetCpus
}

// CPUShares returns the CPU shares (relative weight)
func (r *ContainerConfig) CPUShares() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cpuShares
}

// CPUQuota returns the CPU quota in microseconds
func (r *ContainerConfig) CPUQuota() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cpuQuota
}

// CPUPeriod returns the CPU period in microseconds
func (r *ContainerConfig) CPUPeriod() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cpuPeriod
}

// MemoryLimit returns the memory limit in bytes
func (r *ContainerConfig) MemoryLimit() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.memoryLimit
}

// MemoryReservation returns the memory soft limit in bytes
func (r *ContainerConfig) MemoryReservation() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.memoryReservation
}

// MemorySwap returns the memory + swap limit in bytes
func (r *ContainerConfig) MemorySwap() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.memorySwap
}

// MemoryKernel returns the kernel memory limit in bytes
func (r *ContainerConfig) MemoryKernel() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.memoryKernel
}

// MemorySwappiness returns the memory swappiness setting (0-100)
func (r *ContainerConfig) MemorySwappiness() *uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.memorySwappiness
}

// OOMKillDisable returns whether OOM killer is disabled
func (r *ContainerConfig) OOMKillDisable() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.oomKillDisable
}

// ContainerConfig contains:
//
//	extraLabels: map[string]string; additional labels
//	relativePath: string; the resolved firmware path must be valid
//	pedestal: *Pedestal; the pedestal type must be specified
//	os: string; one of the allowed os
//	ncpu: int; requested CPU count
//	cpuLimit: int; CPU limit from OCI spec
//	cpu: int; allocated CPU (-1 if not allocated)
func (c *Container) validMicaContainer() bool {

	osValid := validOS(c.config.OS())
	fwValid := validFirmware(c.bundle, c.config.FirmwarePath())
	pedValid := hostPedMatched(c.config.Ped())
	compatValid := validCompatibility(c.config)
	judge := osValid && fwValid && pedValid && compatValid

	log.Debugf(`MicaContainer validation result = 
		osValid = %v,
		fwValid = %v,
		pedValid = %v,
		compatValid = %v,
		judge = %v
	`, osValid, fwValid, pedValid, compatValid, judge)
	return judge
}

func (c *Container) GetConfig() *ContainerConfig {
	return c.config
}

func (c *Container) allocClientCPU() error {
	// Use container-specific CPU limit instead of global HostMaxCPU
	cpu, err := allocCPUWithLimit(c.config.ncpu, c.config)
	if err != nil {
		return err
	}
	c.config.cpu = cpu
	return nil
}

func (c *Container) GetClientCPU() (int, error) {
	if c.config.cpuUnset() {
		if err := c.allocClientCPU(); err != nil {
			return c.config.cpu, err
		}
	}
	log.Debugf("get clientcpu done.")
	return c.config.cpu, nil
}

// FUTURE: configure runtime from :
// 1. annotation
// 2. config file
func getRuntimeConfig(ocispec *specs.Spec) (*oci.RuntimeConfig, error) {
	// Parse runtime configuration from OCI spec annotations
	runtimeConfig := oci.ParseRuntimeConfig(ocispec.Annotations)

	log.Pretty("Parsed runtime config: %v", runtimeConfig)
	return runtimeConfig, nil
}



func LoadContainerState(bundle string) (*Container, error) {
	statePath := filepath.Join(bundle, "state.json")
	state, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read container state from %s: %w", statePath, err)
	}
	var container Container
	if err := json.Unmarshal(state, &container); err != nil {
		return nil, fmt.Errorf("failed to unmarshal container state from %s: %w", statePath, err)
	}
	return &container, nil
}
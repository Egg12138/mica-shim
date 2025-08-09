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

	return parsedFields, nil
}

// ContainerConfig represents all configurations parsed from bundle
// This includes OCI spec, MICA-specific configurations, and runtime state
type ContainerConfig struct {
	// OCI Specification
	Spec specs.Spec `json:"spec"`

	// Bundle information
	Bundle string        `json:"bundle"`
	Type   ContainerType `json:"type"`
	Detach bool          `json:"detach"`

	// BUG: overlapped fields: remove MicaConf, leaving extracted MicaLabels and ExtraLabels
	// Parsed configuration values
	// Firmware and pedestal
	// MICA-specific configurations from client.conf
	ExtraLabels  map[string]string `json:"extra_labels"`
	RelativePath string            `json:"relative_path"`
	PedestalType PedType           `json:"pedestal_type"`
	PedestalConf string            `json:"pedestal_conf"`
	OS           string            `json:"os"`
	NCpu         int               `json:"ncpu"` // requested CPU count (default = 1)

	CpuLimit   int    `json:"cpu_limit"`   // CPU limit from OCI spec
	CpusetCpus string `json:"cpuset_cpus"` // cpuset.cpus specification
	CpuShares  uint64 `json:"cpu_shares"`  // CPU shares (relative weight)
	CpuQuota   int64  `json:"cpu_quota"`   // CPU quota in microseconds
	CpuPeriod  uint64 `json:"cpu_period"`  // CPU period in microseconds

	// Memory resource limits from OCI spec
	MemoryLimit       int64   `json:"memory_limit"`       // Memory limit in bytes
	MemoryReservation int64   `json:"memory_reservation"` // Memory soft limit in bytes
	MemorySwap        int64   `json:"memory_swap"`        // Memory + swap limit in bytes
	MemoryKernel      int64   `json:"memory_kernel"`      // Kernel memory limit in bytes
	MemorySwappiness  *uint64 `json:"memory_swappiness"`  // Memory swappiness (0-100)
	OomKillDisable    bool    `json:"oom_kill_disable"`   // Whether to disable OOM killer

	cpu int // allocated CPU (-1 if not allocated) after stat loaded

	mu sync.RWMutex
}

type PedType int

// openamp and jailhouse are not supported yet
const (
	Xen PedType = iota
	// maybe
	ACRN
	Unsupported
)

// String returns the string representation of PedType
func (p PedType) String() string {
	switch p {
	case Xen:
		return "xen"
	default:
		return "unknown"
	}
}

func ParsePedType(s string) PedType {
	switch strings.ToLower(s) {
	case "xen", "":
		return Xen
	default:
		return Unsupported // default to baremetal
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

// TALK: is it needed to add a sandbox state field inside container struct?
type SandboxState struct {
}

type Container struct {
	// dynamic fields
	exitTime time.Time
	io       *libmica.MicaIO
	exitCode uint32

	// states:
	// static fields
	bundle string
	ID     string
	// int32: RUNNING, STOPPED, PAUSED, PAUSING, CREATED, UNKNOWN...
	// preset static fields
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
		ExtraLabels: make(map[string]string),

		// Initialize with defaults
		RelativePath: "",
		PedestalType: Unsupported,
		PedestalConf: "",
		OS:           "",
		NCpu:         1,
		CpuLimit:     0,
		CpusetCpus:   "",
		CpuShares:    0,
		CpuQuota:     0,
		CpuPeriod:    0,

		// Memory defaults
		MemoryLimit:       0,
		MemoryReservation: 0,
		MemorySwap:        0,
		MemoryKernel:      0,
		MemorySwappiness:  nil,
		OomKillDisable:    false,

		cpu: -1, // not allocated yet

		mu: sync.RWMutex{},
	}

	if err := config.parseMicaLabels(micaConf); err != nil {
		return nil, err
	}

	if err := config.parseOCICPUResources(&ocispec); err != nil {
		return nil, err
	}

	if err := config.parseOCIMemoryResources(&ocispec); err != nil {
		return nil, err
	}

	// Validate resource limits against system constraints
	if err := validateResourceLimits(config); err != nil {
		log.Warnf("Resource validation warning: %v", err)
		// Don't fail the container creation for resource validation warnings
		// but log them for visibility
	}

	// Set default OS if not specified
	if config.OS == "" {
		log.Warn("os is not set, default to zephyr")
		config.OS = "zephyr"
	}

	log.Infof("Container resource limits - CPU: %s, Memory: %s",
		formatCPULimit(config), formatMemoryLimit(config))
	return config, nil
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
	if err != nil {
		return nil, fmt.Errorf("failed to load oci spec: %w", err)
	}
	rtConfig, err := getRuntimeConfig(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to load runtime config: %w", err)
	}
	log.Pretty("runtime config: %v", rtConfig)
	enableTTy := spec.Process.Terminal
	// when tty is disable, stdio use regular pipe, which containerd needs pipe io to log
	disableOutput := detach && enableTTy
	// TALK: act like kata?
	// runtimeConfig, err := parseRuntimeConfig(r, spec.Annotations)

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

	if !container.validMicaContainer() {
		return nil, fmt.Errorf("invalid mica container: %+v", container)
	}

	return container, nil
}

// Do not handle unmatched labels here
func (r *ContainerConfig) parseMicaLabels(labels map[string]string) error {
	// TODO: make sure we do can find the firmware path in container bundle

	for k, v := range labels {
		switch k {
		case defs.Firmware:
			r.RelativePath = filepath.Join("rootfs", v)
		case defs.Pedestal:
			r.PedestalType = ParsePedType(v)
		case defs.PedestalConf:
			r.PedestalConf = v
		case defs.OS:
			if v == "" {
				return fmt.Errorf("missing os label")
			}
			if !validOS(v) {
				return fmt.Errorf("invalid os label: %s", v)
			}
			r.OS = v
		case defs.Ncpu:
			r.NCpu = getNcpu(v)
		default:
			if strings.HasPrefix(k, prefix) {
				r.ExtraLabels[k] = v
			}
		}
	}
	return nil
}

// parseOCICPUResources parses CPU resource limits from OCI spec
func (r *ContainerConfig) parseOCICPUResources(spec *specs.Spec) error {
	if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.CPU == nil {
		return nil
	}

	cpu := spec.Linux.Resources.CPU

	// Parse CPU quota and period to get CPU limit
	if cpu.Quota != nil && cpu.Period != nil && *cpu.Period > 0 {
		r.CpuQuota = *cpu.Quota
		r.CpuPeriod = *cpu.Period
		cpuLimit := int(*cpu.Quota / int64(*cpu.Period))
		if cpuLimit > 0 {
			r.CpuLimit = cpuLimit
		}
	}

	if cpu.Shares != nil {
		r.CpuShares = *cpu.Shares
	}

	if cpu.Cpus != "" {
		r.CpusetCpus = cpu.Cpus
	}

	// Parse realtime CPU constraints if present
	if cpu.RealtimeRuntime != nil {
	}

	return nil
}

// parseOCIMemoryResources parses Memory resource limits from OCI spec
func (r *ContainerConfig) parseOCIMemoryResources(spec *specs.Spec) error {
	if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.Memory == nil {
		log.Warn("No Memory resources specified in OCI spec")
		return nil
	}

	memory := spec.Linux.Resources.Memory

	if memory.Limit != nil {
		r.MemoryLimit = *memory.Limit
	}

	if memory.Reservation != nil {
		r.MemoryReservation = *memory.Reservation
	}

	if memory.Swap != nil {
		r.MemorySwap = *memory.Swap
	}

	// Deal with the deprecated field
	if cgroupV1() {
		if memory.Kernel != nil {
			r.MemoryKernel = *memory.Kernel
			log.Infof("Supported only in cgruopv1; parsed kernel memory limit: %d bytes", *memory.Kernel)
		}
	}

	if memory.Swappiness != nil {
		swappiness := uint64(*memory.Swappiness)
		r.MemorySwappiness = &swappiness
	}

	if memory.DisableOOMKiller != nil {
		r.OomKillDisable = *memory.DisableOOMKiller
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

// **************** Setters and Getters ****************

func (r *ContainerConfig) GetFirmwarePath() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.RelativePath
}

// Pedestal returns the pedestal information
func (r *ContainerConfig) GetPed() *Pedestal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return &Pedestal{
		PedestalType: r.PedestalType,
		PedestalConf: r.PedestalConf,
	}
}

// PedestalType returns the pedestal type
func (r *ContainerConfig) GetPedestalType() PedType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.PedestalType
}

// PedestalConf returns the pedestal configuration
func (r *ContainerConfig) GetPedestalConf() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.PedestalConf
}

func (r *ContainerConfig) GetOS() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.OS
}

// GetCompatibility returns compatibility information for a specific component
func (r *ContainerConfig) Compatibility(component string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ExtraLabels[defs.Compat]
}

func (r *ContainerConfig) GetAllLabelsRef() *map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	labels := &r.ExtraLabels
	return labels
}

// cpuUnset is alway callee, hence lock is not needed
func (r *ContainerConfig) cpuUnset() bool {
	return r.cpu == -1
}

type State struct {
	Bundle string           `json:"bundle"`
	ID     string           `json:"id"`
	Status task.Status      `json:"status"`
	CType  ContainerType    `json:"c_type"`
	Config *ContainerConfig `json:"config"`
}

func (c *Container) SetStatus(status task.Status) {
	c.status = status
}

func (c *Container) Status() task.Status {
	return c.status
}

func (c *Container) State() *State {
	s := &State{
		ID:     c.ID,
		Status: c.status,
		Bundle: c.bundle,
		CType:  c.cType,
		Config: c.config,
	}
	return s
}

// NOTICE: Xen is the only supported ped for now
func (c *Container) validMicaContainer() bool {

	osValid := validOS(c.config.GetOS())
	fwValid := validFirmware(c.bundle, c.config.GetFirmwarePath())
	pedValid := hostPedMatched(c.config.GetPed())
	compatValid := validCompatibility(c.config)
	judge := osValid && fwValid && pedValid && compatValid
	log.Debugf(`
		validMicaContainer:
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
	cpu, err := allocCPUWithLimit(c.config.NCpu, c.config)
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
	return c.config.cpu, nil
}

// Container::<static fields>
func (c *Container) SaveState() error {
	failed, failed1 := false, false
	var err error
	var err1 error
	st := c.State()
	stateInBundle := filepath.Join(c.bundle, defs.MicantainerStateFile)
	stateInMicranDir := filepath.Join(defs.MicranStateDir, c.ID, defs.MicantainerStateFile)

	if err = saveStateTo(stateInBundle, st); err != nil {
		failed = true
		err = fmt.Errorf("failed to save state to <%s>: %w", stateInBundle, err)
	}

	if err1 = saveStateTo(stateInMicranDir, st); err1 != nil {
		failed1 = true
		err1 = fmt.Errorf("failed to save state to <%s>: %w", stateInBundle, err1)
	}

	if failed1 && failed {
		return fmt.Errorf("failed to save container state: %w, %w", err, err1)
	}
	return nil
}

// FUTURE: configure runtime from :
// 1. annotation
// 2. config file
func getRuntimeConfig(ocispec specs.Spec) (*oci.RuntimeConfig, error) {
	// Parse runtime configuration from OCI spec annotations
	runtimeConfig := oci.ParseRuntimeConfig(ocispec.Annotations)

	log.Pretty("Parsed runtime config: %v", runtimeConfig)
	return runtimeConfig, nil
}

func saveStateTo(file string, state *State) error {
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal container state: %w", err)
	}
	return os.WriteFile(file, stateBytes, 0644)
}

func RestoreContainerFromState(state *State) (*Container, error) {
	bundle := state.Bundle
	id := state.ID
	status := state.Status
	cType := state.CType
	config := state.Config
	config.cpu = -1
	container := &Container{
		exitTime: time.Time{},
		exitCode: 0,
		io:       nil,
		bundle:   bundle,
		ID:       id,
		status:   status,
		cType:    cType,
		config:   config,
	}
	return container, nil

}

func LoadStateFromDir(baseDir string) (*State, error) {
	var state State
	statePath := filepath.Join(baseDir, defs.MicantainerStateFile)
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read container state from %s: %w", statePath, err)
	}
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal container state from %s: %w", statePath, err)
	}
	return &state, nil
}

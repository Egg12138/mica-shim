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
	"mica-shim/libmica"
	log "mica-shim/logger"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
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
// - ContainerSpec: Raw data from container bundle && metdata
// - MicaContainerInfo: Converted from ContainerSpec
// - libmica.micaClientConf

// ocispec => ContainerSpec => MicaContainerInfo => Container => libmica.micaClientConf

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

// ContainerConf contains OCIspec and fields needde by runtime
// the directly parsed data from container engine container storage system, needed by mica-shim
// then ContainerConf will be injected into ContainerResolution
type ContainerConf struct {
	Spec specs.Spec
	// resolved bundle path
	Bundle   string
	MicaConf map[string]string
	Type     ContainerType
	Detach   bool
}

// MicaContainerInfo contains the resolved container information
// This struct is reusable across the entire shim
type MicaContainerInfo struct {
	extraLabels map[string]string
	// relative firmware path to the bundle. in most cases, it is "rootfs/<firmware_path>"
	relativePath string
	pedestal     *Pedestal
	os           string
	// support single cpu for now
	// mica runtime will allocate CPU when close to libmica.create()
	cpu int
	// default = 1
	ncpu int
	mu   sync.RWMutex
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
	case "baremetal":
		return Baremetal
	case "jailhouse":
		return Jailhouse
	case "xen":
		return Xen
	default:
		return Baremetal // default to baremetal
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
	spec   *ContainerConf
	info   *MicaContainerInfo
}

// *************** Constructors *************** //

func loadSpec(bundle string) (specs.Spec, error) {
	ociSpec, err := parseConfigJSON(bundle)
	if err != nil {
		return specs.Spec{}, err
	}
	return ociSpec, nil
}

// get oci spec and container config from bundle
func parseContainerConf(bundle string, ocispec specs.Spec) (*ContainerConf, error) {
	log.Debugf("recursively walk bundle <%s>: \n %s", bundle, walkDir(bundle))

	time.Sleep(3 * time.Second)

	clientConf, err := parseConfigINI(bundle)
	if err != nil {
		return nil, err
	}

	return &ContainerConf{
		Spec:     ocispec,
		Bundle:   bundle,
		MicaConf: clientConf,
	}, nil
}

// The core container information parser caller
// ContainerInfoParse parses the bundle and metadata, returns a MicaContainerInfo
// This function should be called once per container and the result can be reused
func (conf *ContainerConf) containerInfoParse() (*MicaContainerInfo, error) {
	labels := conf.MicaConf
	result := &MicaContainerInfo{
		extraLabels:  make(map[string]string),
		relativePath: "",
		pedestal:     nil,
		os:           "",
		cpu:          -1,
		ncpu:         1,
		mu:           sync.RWMutex{},
	}

	// CPU and ncpu will be parsed here

	if err := result.parseMicaLabels(labels); err != nil {
		log.Debugf("failed to parse all mica labels: %v", err)
		return nil, err
	}
	if result.os == "" {
		log.Warn("os is not set, default to zephyr")
		result.os = "zephyr"
	}
	log.Debugf("containerInfoParse: %+v", result)
	return result, nil
}

// deprecated: should not parse client.conf and config.json at the same time
// NOTICE: client.conf is parsed after rootfs is mounted, of which oci spec is before
func loadContainerConf(r *taskAPI.CreateTaskRequest, ocispec specs.Spec, detach bool) (*ContainerConf, error) {
	bundlePath, err := validBundle(r.ID, r.Bundle)
	if err != nil {
		return nil, err
	}

	containerSpec, err := parseContainerConf(bundlePath, ocispec)
	if err != nil {
		return nil, err
	}

	containerSpec.Detach = detach

	return containerSpec, nil
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

func SetupContainer(req *taskAPI.CreateTaskRequest) (_ *Container, retErr error) {
	// presetRootfs(req)
	detach := !req.Terminal
	// spec, err := loadContainerConf(req)
	spec, err := loadSpec(req.Bundle)
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

		for _, mnt := range req.Rootfs {
			mounts = append(mounts, mount.Mount{
				Type:    mnt.Type,
				Source:  mnt.Source,
				Target:  mnt.Target,
				Options: mnt.Options,
			})

			rootfsPath := filepath.Join(req.Bundle, "rootfs")
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

	cconf, err := loadContainerConf(req, spec, disableOutput)
	if err != nil {
		return nil, fmt.Errorf("failed to load container conf: %w", err)
	}
	container, err := newContainer(req, *cconf)
	if err != nil {
		return nil, fmt.Errorf("failed to create mica container instance: %w", err)
	}
	return container, err
}

// A new container instance, initialized only :
// 1. checked bundle path
// 2. parsed container spec
// 3. parsed container info
func newContainer(r *taskAPI.CreateTaskRequest, cconf ContainerConf) (*Container, error) {
	info, err := cconf.containerInfoParse()
	if err != nil {
		return nil, err
	}

	container := &Container{
		bundle:   cconf.Bundle,
		ID:       r.ID,
		io:       nil,
		exitCode: 0,
		cType:    cconf.Type,
		spec:     &cconf,
		info:     info,
		// status: remain empty
	}
	log.Debugf("new container: %+v", container)

	if !container.validMicaContainer() {
		return nil, fmt.Errorf("invalid mica container: %+v", container)
	}

	return container, nil
}

// Do not handle unmatched labels here
func (r *MicaContainerInfo) parseMicaLabels(labels map[string]string) error {
	// TODO: make sure we do can find the firmware path in container bundle
	// Parse firmware path
	// preserved os:
	// "zephyr", "uniproton", "linux"

	for k, v := range labels {
		switch k {
		case defs.Firmware:
			r.relativePath = filepath.Join("rootfs", v)
		case defs.Pedestal:
			if r.pedestal != nil {
				r.pedestal.PedestalType = ParsePedType(v)
			} else {
				r.pedestal = &Pedestal{
					PedestalType: ParsePedType(v),
					PedestalConf: "",
				}
			}
		case defs.PedestalConf:
			if r.pedestal != nil {
				r.pedestal.PedestalConf = v
			} else {
				r.pedestal = &Pedestal{
					PedestalType: Unknown,
					PedestalConf: v,
				}
			}
		case defs.OS:
			if v == "" {
				return fmt.Errorf("missing os label")
			}
			log.FDebugf("current os label: %s", v)
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
	return nil
}

// parseDockerConfigJSON tries to parse bundle information from the bundle directory
// For docker, we can get the config.v2.json
// But for containerd, we need to use the containerd API to fetch metadata stored in bolt db
func parseDockerConfigJSON(bundle string) (*ContainerConf, error) {
	dockerConfigPath := filepath.Join(bundle, "config.v2.json")
	if _, err := os.Stat(dockerConfigPath); err == nil {
		dockerConfigData, err := os.ReadFile(dockerConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config.v2.json: %v", err)
		}
		var containerConfigs ContainerConf
		if err := json.Unmarshal(dockerConfigData, &containerConfigs); err != nil {
			return nil, fmt.Errorf("failed to parse config.v2.json: %v", err)
		}
		return &containerConfigs, nil
	}
	return nil, nil
}

// workaround for fetching metadata from containerd, we refuse to call the standard
// containerd API to fetch metadata stored in bolt db
func parseContainerdContainerMetadata(cid string) (*ContainerConf, error) {
	// TODO: a bad implementation, work as a containerd client
	return nil, nil
}

func parseiSuladContainerConfig(bundle string) (*ContainerConf, error) {
	// TODO: parse isulad container config
	return nil, nil
}

func (r *MicaContainerInfo) FirmwarePath() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.relativePath
}

// Pedestal returns the pedestal information
func (r *MicaContainerInfo) Ped() *Pedestal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pedestal
}

func (r *MicaContainerInfo) OS() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.os
}

// // Schedule a CPU when CPU() is called
// func (r *MicaContainerInfo) CPU() int {
// 	r.mu.RLock()
// 	defer r.mu.RUnlock()
// 	cpu := allocCPU()
// 	if err != nil {
// 		return 0, err
// 	}
// 	return cpu, nil
// }

// GetCompatibility returns compatibility information for a specific component
func (r *MicaContainerInfo) Compatibility(component string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.extraLabels[prefix+".client.compatibility."+component]
}

func (r *MicaContainerInfo) GetAllLabelsRef() *map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	labels := &r.extraLabels
	return labels
}

// cpuUnset is alway callee, hence lock is not needed
func (r *MicaContainerInfo) cpuUnset() bool {
	return r.cpu == -1
}

//	MicaContainerInfo: {
//		extraLabels: map[string]string; do not care
//		relativePath: string; the resolved path must be valid
//		pedestal: *Pedestal; the pedestal type must be specified
//		os: string; one of the allowed os
//		ncpu: int
//	}
func (c *Container) validMicaContainer() bool {
	log.Debugf("validating MicaContainer: %+v", c.info)
	judge := validOS(c.info.OS()) &&
		validFirmware(c.spec.Bundle, c.info.FirmwarePath()) &&
		validCompatibility(c.info) &&
		hostPedMatched(c.info.Ped(), c.info.OS())
	log.Debugf("MicaContainer validation result: %v", judge)
	return judge
}

func (c *Container) GetMicaContainerInfo() *MicaContainerInfo {
	return c.info
}

func (c *Container) allocClientCPU() error {
	cpu, err := allocCPU(c.info.ncpu)
	if err != nil {
		return err
	}
	c.info.cpu = cpu
	return nil
}
func (c *Container) GetClientCPU() (int, error) {
	// RW lock
	c.info.mu.Lock()
	defer c.info.mu.Unlock()
	if c.info.cpuUnset() {
		if err := c.allocClientCPU(); err != nil {
			return c.info.cpu, err
		}
	}
	return c.info.cpu, nil
}

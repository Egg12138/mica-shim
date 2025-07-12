package cntr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	defs "mica-shim/definitions"
	"mica-shim/libmica"
	log "mica-shim/logger"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// alternative: docker, containerd, isulad
// high level container engine
var (
	HighLevelCE = "containerd"
	RequiredLabels = []string{
		defs.SuffixFirmware,
		defs.SuffixOS,
	}
)

const prefix = defs.MicaAnnotationPrefix

// Structures:
// - OCISpec: specs.Spec in config.json (or config.v2.json)
// - ContainerSpec: Raw data from container bundle && metdata
// - MicaContainerInfo: Converted from ContainerSpec
// - libmica.micaClientConf

// ocispec => ContainerSpec => MicaContainerInfo => Container => libmica.micaClientConf

// *************** ocispec *************** //
// assume containerd, parse config.json from bundle
// TODO: iSulad
func ParseConfigJSON(bundle string) (specs.Spec, error) {
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

// ContainerSpec contains OCIspec and fields needde by runtime
// the directly parsed data from container engine container storage system, needed by mica-shim
// then ContainerSpec will be injected into ContainerResolution
type ContainerSpec struct {
	Spec   specs.Spec
	// resolved bundle path
	Bundle string
	Labels map[string]string
}

// MicaContainerInfo contains the resolved container information
// This struct is reusable across the entire shim
type MicaContainerInfo struct {
	extraLabels     map[string]string
	// relative firmware path to the bundle. in most cases, it is "rootfs/<firmware_path>"
	relativePath string
	pedestal     *Pedestal
	os           string
	// support single cpu for now
	cpu          uint32
	// default = 1
	ncpu         int
	mu           sync.RWMutex
}

type PedType int

const (
	Baremetal PedType = iota + 1
	Jailhouse
	Xen
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
	SideCar ContainerType = "sidecar"
	// a regular containers created not by CRI
	Regular ContainerType = "regular"
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
	status   task.Status
	cType    ContainerType
	spec     *ContainerSpec
	info     *MicaContainerInfo
}

// *************** Constructors *************** //

func getContainerSpec(bundle string) (*ContainerSpec, error) {
	ociSpec, err := ParseConfigJSON(bundle)
	if err != nil {
		return nil, err
	}

	return &ContainerSpec{
		Spec:   ociSpec,
		Bundle: bundle,
		Labels: make(map[string]string),
	}, nil
}


// ContainerInfoParse parses the bundle and metadata, returns a ContainerResolution
// This function should be called once per container and the result can be reused
func (spec *ContainerSpec) containerInfoParse() (*MicaContainerInfo, error) {
	labels := spec.Labels
	result := &MicaContainerInfo{
		extraLabels:     make(map[string]string),
		relativePath: "",
		pedestal:     nil,
		os:           "",
		cpu:          0,
		ncpu:         1,
		mu:           sync.RWMutex{},
	}

	result.parseMicaLabels(labels)
	return result, nil
}


func LoadContainerSpec(r *taskAPI.CreateTaskRequest) (*ContainerSpec , error) {

	bundlePath, err := ValidBundle(r.ID, r.Bundle)
	if err != nil {
		return nil, err
	}

	containerSpec, err := getContainerSpec(bundlePath)

	if err != nil {
		return nil, err
	}

	return containerSpec, nil
}

func GetContainerType(spec *ContainerSpec) (ContainerType, error) {
	ocispec := spec.Spec
	for _, key := range CRIContainerTypeKeyList {
		containerType, ok := ocispec.Annotations[key]
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

// A new container instance, initialized only :
// 1. checked bundle path
// 2. parsed container spec
// 3. parsed container info
func NewContainer(r *taskAPI.CreateTaskRequest, spec ContainerSpec ,  cT ContainerType) (*Container, error) {
	info, err := spec.containerInfoParse()
	if err != nil {
		return nil, err
	}
	
	container := &Container{
		bundle:   spec.Bundle,
		ID:       r.ID,
		io:       nil,
		exitCode: 0,
		cType:    cT,
		spec:     &spec,
		info:     info,
		// status: remain empty
	}

	return container, nil
}

func (spec *ContainerSpec) getAllMicaLabels() map[string]string {
	labels := make(map[string]string)
	for k, v := range spec.Labels {
		if strings.HasPrefix(k, prefix) {
			labels[k] = v
		}
	}
	return labels
}


// Do not handle unmatched labels here 
func (r *MicaContainerInfo) parseMicaLabels(labels map[string]string) error {
	// TODO: make sure we do can find the firmware path in container bundle
	// Parse firmware path
	// preserved os:
	// "zephyr", "uniproton", "linux"

	for k, v := range labels {
		switch k {
			case prefix + defs.SuffixFirmware:
				r.relativePath = filepath.Join("rootfs", v)
			case prefix + defs.SuffixPedestal:
				r.pedestal = &Pedestal{
					PedestalType: ParsePedType(v),
					PedestalConf: r.extraLabels[prefix+".client.pedestal_conf"],
				}
			case prefix + defs.SuffixOS:
				if v == "" {
					return fmt.Errorf("missing os label")
				}
				r.os = v
			case prefix + defs.SuffixNcpu:
				ncpu, err := strconv.Atoi(v)
				if err != nil {
					log.Warnf("failed to parse ncpu(int) label from %s: %v", v, err)
					r.ncpu = 1
				} else {
					r.ncpu = ncpu
				}
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
func parseDockerConfigJSON(bundle string) (*ContainerSpec, error) {
	dockerConfigPath := filepath.Join(bundle, "config.v2.json")
	if _, err := os.Stat(dockerConfigPath); err == nil {
		dockerConfigData, err := os.ReadFile(dockerConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config.v2.json: %v", err)
		}
		var containerConfigs ContainerSpec
		if err := json.Unmarshal(dockerConfigData, &containerConfigs); err != nil {
			return nil, fmt.Errorf("failed to parse config.v2.json: %v", err)
		}
		return &containerConfigs, nil
	}
	return nil, nil
}

func parseContainerdContainerMetadata(cid string) (*ContainerSpec, error) {
	// TODO: parse bultdb
	return nil, nil
}

func parseiSuladContainerConfig(bundle string) (*ContainerSpec, error) {
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

func (r *MicaContainerInfo) CPU() uint32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cpu
}

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

// MicaContainerInfo: {
// 	extraLabels: map[string]string; do not care
// 	relativePath: string; the resolved path must be valid
// 	pedestal: *Pedestal; the pedestal type must be specified
// 	os: string; one of the allowed os
// 	ncpu: int
// }
func (c *Container) validMicaContainer() bool {
	return validOS(c.info.OS()) &&
		validFirmware(c.spec.Bundle, c.info.FirmwarePath()) &&
		validCompatibility(c.info) &&
		hostPedMatched(c.info.Ped(), c.info.OS())
}

// IsMicaImage returns true if this is a mica image
func (c *Container) IsMicaImage() bool {
	return c.validMicaContainer()
}

func (c *Container) GetMicaContainerInfo() *MicaContainerInfo {
	return c.info
}

func (c *Container) AllocClientCPU() error {
	cpu, err := allocCPU()
	if err != nil {
	}
}

// *************** Utils functions *************** //

// TODO: multi cpu
// sharememory-based scheduler
func allocCPU() (uint32, error) {
	// TODO: alloc cpu
	return 1, nil
}

// check OS value matches
func validOS(os string) bool {
	val := false
	for _, o := range defs.PreservedOS {
		if o == os {
			val = true
			break
		}
	}
	log.Debugf("current os lable is: %s. valid = %v", os, val)
	return val
}

func validFirmware(root, firmware string) bool {
	// <bundle>/rootfs/<firmware>
	resolved, err := resolvePath(filepath.Join(root, firmware))
	if err != nil {
		return false
	}
	ret := fileExists(resolved)
	log.Debugf("current firmware path is: %s. valid = %v", resolved, ret)
	return ret
}

func validCompatibility(info *MicaContainerInfo) bool {
	// TODO: needed to ? how to check compatibility?
	return true
}

func hostPed() *Pedestal {
	// TODO: get host pedestal
	return &Pedestal{
		PedestalType: Baremetal,
		PedestalConf: "",
	}
}


// Currently, one host only support one pedestal type.
func hostPedMatched(ped *Pedestal, os string) bool {
	currentHost := hostPed()
	if currentHost.PedestalType != ped.PedestalType {
		return false
	}
	return true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

func setReadonly(path string) error {
	// assume path is a valid direntry
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0444)
		if info.IsDir() {
			mode = os.FileMode(0555)
		}
		return os.Chmod(path, mode)
	})
}

// bundle is <CONTINAER_STATE_ROOT>/<container_id>
func SetupBundle(bundle string) error {

	// config := filepath.Join(bundle, "config.json")
	rootfs := filepath.Join(bundle, "rootfs")

	rootfsExists := fileExists(rootfs)
	log.Debugf("rootfs <%s> Exists: %v", rootfs, rootfsExists)
	// TODO: mount rootfs
	if !rootfsExists {
		if err := os.MkdirAll(rootfs, 0755); err != nil {
			return fmt.Errorf("failed to create rootfs: %w", err)
		}
	}

	// TODO: recursively chmod 0555
	if err := setReadonly(rootfs); err != nil {
		return fmt.Errorf("failed to chmod rootfs: %w", err)
	}
	os.Chdir(bundle)
	return nil
}

func ValidBundle(containerID, bundlePath string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("missing container ID")
	}

	if bundlePath == "" {
		return "", fmt.Errorf("missing bundle path")
	}

	// bundle path MUST be valid.
	fileInfo, err := os.Stat(bundlePath)
	if err != nil {
		return "", fmt.Errorf("invalid bundle path '%s': %s", bundlePath, err)
	}
	if !fileInfo.IsDir() {
		return "", fmt.Errorf("invalid bundle path '%s', it should be a directory", bundlePath)
	}

	// get a valid expanded path
	resolved, err := resolvePath(bundlePath)
	if err != nil {
		return "", err
	}

	return resolved, nil
}

// resolvePath returns the fully resolved and expanded value of the
// specified path.
func resolvePath(path string) (string, error) {
	if path == "" {
		log.LocateDebugf("path must be specified")
		return "", fmt.Errorf("path must be specified")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file %v does not exist", absolute)
		}

		return "", err
	}

	return resolved, nil
}

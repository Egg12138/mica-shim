package oci

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	defs "mica-shim/definitions"
	log "mica-shim/logger"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// alternative: docker, containerd, isulad
// high level container engine
var HighLevelCE = "containerd"

const micaprefix = defs.MicaAnnotationPrefix


// ContainerResolution contains the resolved container information
// This struct is reusable across the entire shim
type ContainerResolution struct {
	bundlePath    string
	imageHash     string
	labels   map[string]string
	
	// datas for libmica, from 'annotations' field
	firmwarePath  string
	pedestal      *Pedestal
	
	// Thread safety
	mu            sync.RWMutex
	parsed        bool
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

// the directly parsed data from container engine container storage system, needed by mica-shim
// then ContainerOCISpec will be injected into ContainerResolution
type ContainerOCISpec struct {
	Spec specs.Spec `json:"spec"`
}

// ContainerInfoParse parses the bundle and returns a BundleResolutionResult
// This function should be called once per bundle and the result can be reused
func ContainerInfoParse(bundle string) (*ContainerResolution, error) {
	result := &ContainerResolution{
		bundlePath:  bundle,
		labels: make(map[string]string),
	}
	
	//  parse labels

	// TODO: use HighLevelCE to decide which container engine is used
	dockerCntrConf, err := parseDockerConfigJSON(bundle)
	if err != nil {

	} else if dockerCntrConf != nil {
		// hit: take docker as CE
		// HighLevelCE = "docker"
		result.labels = dockerCntrConf.Labels
	} else if err != nil {
		return nil, err
	} else {
		// hit: take containerd as CE
		// HighLevelCE = "containerd"
		containerdConf, err := parseContainerdContainerMetadata(bundle)
		if err == nil && containerdConf != nil {
			result.labels = containerdConf.Labels
		} else {
			return nil, err
		}
	}
	
	result.parseMicaLabels()
	result.parsed = true
	
	return result, nil
}

// assume containerd, 
func GetOCISpec(bundle string) (specs.Spec, error) {
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

// parseDockerConfigJSON tries to parse bundle information from the bundle directory
// For docker, we can get the config.v2.json
// But for containerd, we need to use the containerd API to fetch metadata stored in bolt db
func parseDockerConfigJSON(bundle string) (*ContainerOCISpec, error) {
	dockerConfigPath := filepath.Join(bundle, "config.v2.json")
	if _, err := os.Stat(dockerConfigPath); err == nil {
		dockerConfigData, err := os.ReadFile(dockerConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config.v2.json: %v", err)
		}
		var containerConfigs ContainerOCISpec
		if err := json.Unmarshal(dockerConfigData, &containerConfigs); err != nil {
			return nil, fmt.Errorf("failed to parse config.v2.json: %v", err)
		}
		return &containerConfigs, nil
	}
	return nil, nil
}

func parseContainerdContainerMetadata(cid string) (*ContainerOCISpec, error) {
	// TODO: parse bultdb
	return nil, nil
}

func parseiSuladContainerConfig(bundle string) (*ContainerOCISpec, error) {
	// TODO: parse isulad container config
	return nil, nil
}


func (r *ContainerResolution) parseMicaLabels() {
	// TODO: make sure we do can find the firmware path in container bundle
	// Parse firmware path
	if firmwareLabel, exists := r.labels[micaprefix+".client.firmware"]; exists && firmwareLabel != "" {
		// TODO: makeby not in bundlePath, instead, the the image content dir
		r.firmwarePath = filepath.Join(r.bundlePath, "rootfs", firmwareLabel)
	}
	
	pedestalType := ParsePedType(r.labels[micaprefix+".client.pedestal"])
	pedestalConf := r.labels[micaprefix+".client.pedestal_conf"]
	
	r.pedestal = &Pedestal{
		PedestalType: pedestalType,
		PedestalConf: pedestalConf,
	}
}

func (r *ContainerResolution) BundleDir() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bundlePath
}

func (r *ContainerResolution) FirmwarePath() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.firmwarePath
}

// Pedestal returns the pedestal information
func (r *ContainerResolution) Ped() *Pedestal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pedestal
}

// ImageName returns the image name
func (r *ContainerResolution) Image() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.imageHash
}

func (r *ContainerResolution) PlatformConfig() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.labels[micaprefix+".client. platform_config"]
}

func (r *ContainerResolution) OS() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.labels[micaprefix+".client.os"]
}


func (r *ContainerResolution) Description() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return  r.labels["description"]
}

// GetCompatibility returns compatibility information for a specific component
func (r *ContainerResolution) GetCompatibility(component string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.labels[micaprefix+".client.compatibility."+component]
}

// GetMicaLabel returns a specific mica label value
// PROVIDED for runtime plugins
func (r *ContainerResolution) GetMicaLabel(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	// Add prefix if not present
	if !strings.HasPrefix(key, micaprefix) {
		key = micaprefix + "." + key
	}
	
	return r.labels[key]
}

func (r *ContainerResolution) GetAllMicaLabels() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	labels := make(map[string]string)
	for k, v := range r.labels {
		if strings.HasPrefix(k, micaprefix) {
			labels[k] = v
		}
	}
	return labels
}

func (r *ContainerResolution) validMicaContainer() bool {
	validMica := func (m map[string]string, keys[]string) bool {
		for _, key := range keys {
			if _, exists := m[key]; !exists {
				return false
			}
		}
		return true
	}(r.labels, []string{
		micaprefix+".client.os",
		micaprefix+".client.firmware",
	})

	return validMica
}

// IsMicaImage returns true if this is a mica image
func (r *ContainerResolution) IsMicaImage() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.validMicaContainer()
}

func (r *ContainerResolution) GetAllLabelsRef() *map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	labels := &r.labels
	return labels
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) { return false }
	return true
}

func setReadonly(path string) error {
	// assume path is a valid direntry
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil { return err }
		mode := os.FileMode(0444)
		if info.IsDir() { mode = os.FileMode(0555) }
		return os.Chmod(path, mode)
	})
}


// bundle is <CONTINAER_STATE_ROOT>/<container_id>
func SetupBundle(bundle string)  error {

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
		return "", fmt.Errorf("Missing container ID")
	}

	if bundlePath == "" {
		return "", fmt.Errorf("Missing bundle path")
	}

	// bundle path MUST be valid.
	fileInfo, err := os.Stat(bundlePath)
	if err != nil {
		return "", fmt.Errorf("Invalid bundle path '%s': %s", bundlePath, err)
	}
	if !fileInfo.IsDir() {
		return "", fmt.Errorf("Invalid bundle path '%s', it should be a directory", bundlePath)
	}

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
		return "", fmt.Errorf("path must be specified")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			// Make the error clearer than the default
			return "", fmt.Errorf("file %v does not exist", absolute)
		}

		return "", err
	}

	return resolved, nil
}
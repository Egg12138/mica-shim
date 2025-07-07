package cntr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	defs "mica-shim/definitions"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const micaprefix = defs.MicaAnnotationPrefix

// ContainerResolution contains the resolved container information
// This struct is reusable across the entire shim
type ContainerResolution struct {
	bundlePath    string
	imageName     string
	annotations   map[string]string
	imageManifest *ocispec.Manifest
	
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

// ParsePedType parses string to PedType
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

// ContainerInfoParse parses the bundle and returns a BundleResolutionResult
// This function should be called once per bundle and the result can be reused
func ContainerInfoParse(bundle string) (*ContainerResolution, error) {
	result := &ContainerResolution{
		bundlePath:  bundle,
		annotations: make(map[string]string),
	}
	
	if err := result.parseDockerBundle(); err != nil {
		// if err2 := result.parseContainerdMetadb(); err2 != nil {
		// 	return nil, fmt.Errorf("failed to parse bundle from directory (%v) and containerd API (%v)", err, err2)
		// }
	}
	
	result.parseMicaLabels()
	result.parsed = true
	
	return result, nil
}

// parseDockerBundle tries to parse bundle information from the bundle directory
// For docker, we can get the config.v2.json
// But for containerd, we need to use the containerd API to fetch metadata stored in bolt db
func (r *ContainerResolution) parseDockerBundle() error {
	configPath := filepath.Join(r.bundlePath, "config.v2.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("config.v2.json not found in bundle directory %s", r.bundlePath)
	}
	
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config.v2.json: %v", err)
	}
	
	var config struct {
		Process struct {
			Args []string `json:"args"`
		} `json:"process"`
		Annotations map[string]string `json:"annotations"`
	}
	
	if err := json.Unmarshal(configData, &config); err != nil {
		return fmt.Errorf("failed to parse config.v2.json: %v", err)
	}
	
	// Extract annotations (labels)
	r.annotations = config.Annotations
	if config.Annotations != nil {
		r.annotations = config.Annotations
	}
	
	return nil
}


func (r *ContainerResolution) parseMicaLabels() {
	// TODO: make sure we do can find the firmware path in container bundle
	// Parse firmware path
	if firmwareLabel, exists := r.annotations[micaprefix+".client.firmware"]; exists && firmwareLabel != "" {
		// TODO: makeby not in bundlePath, instead, the the image content dir
		r.firmwarePath = filepath.Join(r.bundlePath, "rootfs", firmwareLabel)
	}
	
	pedestalType := ParsePedType(r.annotations[micaprefix+".client.pedestal"])
	pedestalConf := r.annotations[micaprefix+".client.pedestal_conf"]
	
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
	return r.imageName
}

func (r *ContainerResolution) PlatformConfig() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.annotations[micaprefix+".client. platform_config"]
}

func (r *ContainerResolution) OS() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.annotations[micaprefix+".client.os"]
}


func (r *ContainerResolution) Description() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return  r.annotations["description"]
}

// GetCompatibility returns compatibility information for a specific component
func (r *ContainerResolution) GetCompatibility(component string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.annotations[micaprefix+".client.compatibility."+component]
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
	
	return r.annotations[key]
}

func (r *ContainerResolution) GetAllMicaLabels() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	labels := make(map[string]string)
	for k, v := range r.annotations {
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
	}(r.annotations, []string{
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
	labels := &r.annotations
	return labels
}




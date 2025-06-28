package cntr

import (
	"encoding/json"
	defs "mica-shim/definitions"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const prefix = defs.MicaAnnotationPrefix

type MicaImageResolver struct {
	client *containerd.Client
}

type PedestalImageMapping struct {
	BaseImage string `json:"base_image"`
	// mapping: ped =>  baremetal | jailhouse | xen
	Pedestal map[string]string `json:"pedestal"`
	Image string `json:"image"`
}

// ImageResolutionRequest contains the request for image resolution
type ImageResolutionRequest struct {
	ImageName   string            `json:"image_name"`   // Requested image (e.g., "zephyr:latest")
	Ped        string            `json:"ped"`         // Pedestal type from pod spec
	Annotations map[string]string `json:"annotations"`  // Additional pod/container annotations
}

// ImageResolutionResult contains the resolved image information
type ImageResolutionResult struct {
	ResolvedImage   string            `json:"resolved_image"`   // Actual image to use
	OriginalImage   string            `json:"original_image"`   // Original requested image
	SelectedPed     string            `json:"selected_ped"`    // Selected pedestal
	ImageManifest   *ocispec.Manifest `json:"image_manifest"`   // OCI manifest
	Labels          map[string]string `json:"labels"`           // Image labels
	ResolutionPath  string            `json:"resolution_path"`  // How the image was resolved
}

func NewMicaImageResolver(client *containerd.Client) *MicaImageResolver {
	return &MicaImageResolver{client: client}
}

// ReadFirmwarePath 从OCI bundle中读取固件路径并检测mica镜像 
// {prefix}.client.firmware = "${filepath}", absolute path
func ReadFirmwarePath(bundle string) (string, bool, error) {
	configPath := filepath.Join(bundle, "config.json")
	file, err := os.Open(configPath)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	var config struct {
		Annotations map[string]string `json:"annotations"`
	}
	
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return "", false, err
	}

	isMica := false
	firmwarePath := ""
	
	for key, value := range config.Annotations {
		if strings.HasPrefix(key, prefix) {
			isMica = true
			if key == prefix+".client.firmware" && value != "" {
				firmwarePath = filepath.Join(bundle, "rootfs", value)
			}
		}
	}
	
	return firmwarePath, isMica, nil
}

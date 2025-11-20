package configstack

import (
	"path/filepath"
	"strings"

	log "mica-shim/logger"
	"mica-shim/pkg/utils"
)

const clientConfName = "client.conf"

// ContainerLayer captures overrides sourced from client.conf or similar files.
type ContainerLayer struct {
	FirmwarePath string
	PedestalType string
	PedestalConf string
	OS           string
}

// LoadClientLayer parses bundleRootfs/client.conf and returns overrides, if any.
func LoadClientLayer(rootfs string) (ContainerLayer, error) {
	var layer ContainerLayer
	if strings.TrimSpace(rootfs) == "" {
		return layer, nil
	}

	clientConf := filepath.Join(rootfs, clientConfName)
	// whitelist the [Mica] section; utils.ParseConfigINI lowercases section names.
	fields, err := utils.ParseConfigINI(clientConf, []string{"mica"})
	if err != nil {
		log.Warnf("failed to parse %s: %v; continuing without overrides", clientConf, err)
		return layer, nil
	}
	if len(fields) == 0 {
		return layer, nil
	}

	if v := strings.TrimSpace(fields["clientpath"]); v != "" {
		layer.FirmwarePath = v
	}
	if v := strings.TrimSpace(fields["pedestal"]); v != "" {
		layer.PedestalType = v
	}
	if v := strings.TrimSpace(fields["pedestalconf"]); v != "" {
		layer.PedestalConf = v
	}
	if v := strings.TrimSpace(fields["os"]); v != "" {
		layer.OS = v
	}

	log.Debugf("client.conf overrides resolved: %+v", layer)
	return layer, nil
}

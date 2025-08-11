package shim

import (
	"context"
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/fileutils"
	"mica-shim/pkg/oci"
	"os"
	"path/filepath"

	"github.com/container-orchestrated-devices/container-device-interface/specs-go"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

func shimSocketAddr(ctx context.Context, bundle string, opts shimv2.StartOpts) (string, error) {
	bundle, err := validBundle(opts.ID, bundle)
	if err != nil {
		return "", err
	}

	ociSpec := loadSpec(bundle)
	if ociSpec == nil {
		return "", fmt.Errorf("failed to load valid runtime config")
	}

	ctype, err := oci.GetContainerType(ociSpec)

	
}



func loadSpec(bundle string) (*specs.Spec) {
	ociSpec, err := oci.ParseConfigJSON(bundle)
	if err != nil {
		return nil
	}
	return &ociSpec
}



func validBundle(containerID, bundlePath string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("container ID is empty")
	}

	if bundlePath == "" {
		return "", fmt.Errorf("missing bundle path")
	}

	// resolve path first to handle symlinks before other checks
	resolved, err := fileutils.ResolvePath(bundlePath)
	if err != nil {
		return "", err
	}

	stat, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("invalid resolved bundle path '%s': %s", resolved, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("invalid resolved bundle path '%s', it should be a directory", resolved)
	}

	

	return resolved, nil
}

func validRootfs(resolved string) error {
	// always mkdir rootfs inside bundle, whatever containerd use externalrootfs or not
	rootfs := filepath.Join(resolved, "rootfs")
	stat, err := os.Stat(rootfs)

	if err != nil && !os.IsNotExist(err) {
		log.Warnf("failed to stat rootfs")
	}

	if !stat.IsDir() || os.IsNotExist(err) {
		log.Warnf("rootfs path under '%s' is not a directory", resolved)
	}

	if err := setInternalRootfs(resolved); err != nil {
		return fmt.Errorf("failed to set internal rootfs \"%s\": %w", rootfs, err)
	}
}


// bundle is <CONTINAER_STATE_ROOT>/<container_id>
func setInternalRootfs(bundle string) error {

	// config := filepath.Join(bundle, "config.json")
	rootfs := filepath.Join(bundle, "rootfs")

	// TODO: recursively chmod 0555
	if err := fileutils.SetReadonly(rootfs); err != nil {
		return fmt.Errorf("failed to chmod rootfs: %w", err)
	}
	os.Chdir(bundle)
	return nil
}



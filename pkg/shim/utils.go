package shim

import (
	"context"
	"fmt"
	log "mica-shim/logger"
	"mica-shim/pkg/fileutils"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/oci"
	"os"
	"path/filepath"

	"github.com/container-orchestrated-devices/container-device-interface/specs-go"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

// Generate socket address for pod managed by this shim in future
// As for regular container and sandbox, the address will be handled in Create()
func preparePodSocketAddr(ctx context.Context, bundle string, opts shimv2.StartOpts) (string, error) {

	ociSpec := loadOCISpec(bundle)
	if ociSpec == nil {
		return "", fmt.Errorf("failed to load valid runtime config")
	}

	ctype, err := oci.GetContainerType(ociSpec)
	if err != nil {
		return "", err
	}

	if ctype == cntr.PodContainer {
		sandboxID, err := oci.GetSandboxID(ociSpec)
		if err != nil {
			return "", err
		}
		// format: unix://<run_root>/s/<sha256(..)>
		sockAddr, err := shimv2.SocketAddress(ctx, opts.Address, sandboxID)
		if err != nil {
			return "", fmt.Errorf("socket address: %w", err)
		}
		return sockAddr, nil
	}
	return "", nil
}

func loadOCISpec(bundle string) *specs.Spec {
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

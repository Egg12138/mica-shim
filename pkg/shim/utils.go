package shim

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/fileutils"
	"mica-shim/pkg/libmica"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/oci"
	"os"
	"path/filepath"
	"strings"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
	protobuf_types "github.com/gogo/protobuf/types"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Validate the bundle and rootfs.
func validBundle(containerID, bundlePath string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("container ID is empty")
	}

	if bundlePath == "" {
		return "", fmt.Errorf("bundle path is required")
	}

	// resolve path first to handle symlinks before other checks
	resolved, err := fileutils.ResolvePath(bundlePath)
	if err != nil {
		return "", err
	}

	stat, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("invalid resolved bundle path '%s': %w", resolved, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("invalid resolved bundle path '%s', it should be a directory", resolved)
	}

	return resolved, nil
}

func validateRootfs(resolved string) error {
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
	return nil
}

// The bundle is <CONTINAER_STATE_ROOT>/<container_id>.
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

// Generate the socket address for a pod managed by this shim.
// For regular containers and sandboxes, the address will be handled in Create().
func preparePodSocketAddr(ctx context.Context, bundle string, opts shimv2.StartOpts) (string, error) {

	ociSpec, err := oci.LoadSpec(bundle)
	if err != nil {
		return "", fmt.Errorf("failed to load valid runtime config: %w", err)
	}

	ctype, err := oci.GetContainerType(&ociSpec)
	if err != nil {
		return "", err
	}

	if ctype == cntr.PodContainer {
		sandboxID, err := oci.GetSandboxID(&ociSpec)
		if err != nil {
			return "", err
		}
		// format: unix://<run_root>/s/<sha256(..)>
		sockAddr, err := shimv2.SocketAddress(ctx, opts.Address, sandboxID)
		if err != nil {
			return "", fmt.Errorf("failed to generate socket address: %w", err)
		}
		return sockAddr, nil
	}
	return "", nil
}

// Detect if the container is a pause container.
func isPauseContainer(spec *specs.Spec) bool {
	if spec.Process == nil || len(spec.Process.Args) == 0 {
		log.Debugf("spec.Process is nil or empty: %v", spec.Process)
		return false
	}

	pausePatterns := getPausePatterns()

	for _, arg := range spec.Process.Args {
		for _, pattern := range pausePatterns {
			if strings.Contains(arg, pattern) {
				return true
			}
		}
	}

	return false
}

// TODO:
// choose by priority:
// 1. runtime configurated
// 2. alternatives, in defs.
// 3. default k8s.gcr.io/pause
func getPausePatterns() []string {
	return []string{"pause", "/pause", defs.PauseImage}
}

// Handle SCHED_CORE.
func handleSchedCore() {
	log.Infof(`The functions and features of SCHED_CORE can currently be partially accomplished and replaced by Pedestal (default is Xen), 
	and micran does not need it for now. 
	However, in the future, we may provide a more unique way to combine the advantages of SCHED_CORE with the isolation strategy of Pedestal.`)
}


func getMicadPid() uint32 {
	// Get MICA daemon state which includes PID
	daemonState, err := libmica.DaemonState()
	if err != nil {
		log.Warnf("Failed to get micad daemon state: %v", err)
		return 0
	}
	
	// Check if daemon is actually running before returning PID
	if daemonState.State != libmica.DaemonRunning {
		log.Warnf("Micad daemon is not running (state: %s)", daemonState.State)
		return 0
	}
	
	return uint32(daemonState.Pid)
}


func marshalStats(ctx, stat *cntr.ContainerStats)	(*protobuf_types.Any, error) {
	return nil, nil
}

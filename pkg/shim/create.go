// Package shim provides the implementation of the containerd shim v2 interface for micran.
package shim

import (
	"context"
	"fmt"
	defs "micrun/definitions"
	log "micrun/logger"
	"micrun/pkg/configstack"
	cntr "micrun/pkg/micantainer"
	"micrun/pkg/netns"
	"micrun/pkg/oci"
	"micrun/pkg/pedestal"
	"micrun/pkg/utils"
	"os"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go"

	// Only register the proto type.
	crioption "github.com/containerd/containerd/pkg/runtimeoptions/v1"
)

// create is the internal implementation for the Create RPC. It handles sandbox and container creation.
//
// This function orchestrates the entire container/sandbox creation flow, translating containerd requests
// into mica runtime operations. The process involves: **file paths are shown for example**
//
//  1. State Directory Setup: Ensures /tmp/micran (MicranContainerStateDir) exists for runtime state.
//     This directory stores runtime metadata and facilitates communication with mica daemon.
//
//  2. Rootfs Processing: Extracts filesystem mount information from the request.
//     Typically r.Rootfs contains one mount point with source like:
//     "/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots/123/fs"
//     micrun needs to mount it into container directory at "<bundle>/rootfs", see below:
//
//  3. OCI Spec Loading: Loads the OCI runtime specification from config.json in the <bundle> directory.
//     Bundle path structure: "/var/lib/containerd/io.containerd.runtime.v2.task/<namespace>/<container_id>"
//     Example: "/var/lib/containerd/io.containerd.runtime.v2.task/default/test"
//
//  4. Container Type Detection: Determines if this is a PodSandbox (pause container),
//     SingleContainer (ctr/nerdctl created container), or PodContainer (container within a pod, where a sandbox is created).
//     This detection is based on OCI image annotations and CRI configurations.
//
//  5. Runtime Configuration: Loads runtime config from multiple sources with precedence:
//     Micrun shim model is 1:1:1 (Shim : Container : RTOS), so runtime config is for per-shim, not traditional *daemon* level.
//     Hence we support customize per-shim config via annotations or CRI options, not only files
//     - Annotations (highest priority, e.g., "org.openeuler.mica.pedestal=xen")
//     - CRI Options from containerd
//     - Environment variables (defs.MicrunConfEnv)
//     - Default config files in standard locations
//
//  6. Rootfs Mounting: Mounts the container's root filesystem to "<bundle>/rootfs".
//     For non-sandbox containers, traverses and logs the rootfs contents for debugging.
//
// 7. Sandbox/Container Creation: Based on container type:
//
//   - PodSandbox/SingleContainer: Creates new sandbox (calls createSandboxContainer)
//
//   - PodContainer: Adds container to existing sandbox (calls createPodContainer)
//
//     8. Network Namespace Setup: For sandboxes, creates a network namespace managed by nerdctl.
//     The namespace path is stored in annotations as "nerdctl/network-namespace".
//     Network namespace holder PID is tracked for proper lifecycle management.
//
//     9. Container Object Creation: Instantiates the container object with proper typing and,
//     for sandboxes, stores the netns holder PID for state queries.
//
// Typical variable values:
//   - r.ID: "test" (containerd-assigned unique ID)
//   - r.Bundle: "/run/containerd/io.containerd.runtime.v2.task/default/test"
//   - rootfsPath: "/run/containerd/io.containerd.runtime.v2.task/default/test/rootfs"
//   - containerType: cntr.PodSandbox (for pause), cntr.SingleContainer (ctr create), or cntr.PodContainer
//   - Runtime config source: annotation like "org.openeuler.micrun.pedestal=xen"
//
// Returns a container object representing the created sandbox or container, or an error
// if any step in the creation process fails. The function ensures proper cleanup of
// partially created resources on error.
func create(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest) (_ *container, err error) {
	err = setupMicranStateDir()
	if err != nil {
		log.Debugf("failed to set up micran state directory: %w", err)
	}

	rootfs := cntr.RootFs{}
	if len(r.Rootfs) == 1 {
		mnt := r.Rootfs[0]
		rootfs.Source = mnt.Source
		rootfs.Type = mnt.Type
		rootfs.Options = mnt.Options
	}

	detach := !r.Terminal
	ociSpec, bundlePath, err := loadSpec(r.ID, r.Bundle)
	if err != nil {
		return nil, err
	}

	containerType, err := oci.GetContainerType(ociSpec)
	if err != nil {
		return nil, err
	}

	disableOutput := detach && ociSpec.Process.Terminal
	rootfsPath := filepath.Join(r.Bundle, "rootfs")
	runtimeConfig, err := loadRuntimeConfig(s, r, ociSpec.Annotations)
	if err != nil {
		return nil, err
	}

	if err := handleContainerTypeCreation(ctx, s, containerType, r, ociSpec, runtimeConfig, bundlePath, rootfsPath, disableOutput, &rootfs); err != nil {
		return nil, err
	}

	container, err := newContainer(s, r, containerType, ociSpec, rootfs.Mounted)
	if err != nil {
		return nil, err
	}

	if containerType == cntr.PodSandbox && s.sandbox != nil {
		if pid := s.sandbox.NetnsHolderPID(); pid > 0 {
			container.pid = uint32(pid)
		}
	}

	return container, nil
}

func handleContainerTypeCreation(ctx context.Context, s *shimService, containerType cntr.ContainerType,
	r *taskAPI.CreateTaskRequest, ociSpec *specs.Spec, runtimeConfig *oci.RuntimeConfig,
	bundlePath, rootfsPath string, disableOutput bool, rootfs *cntr.RootFs) error {
	switch containerType {
	case cntr.PodSandbox, cntr.SingleContainer:
		return createSandboxContainer(ctx, s, containerType, r, ociSpec, runtimeConfig, bundlePath, rootfsPath, disableOutput, rootfs)
	case cntr.PodContainer:
		return createPodContainer(ctx, s, r, ociSpec, bundlePath, rootfsPath, disableOutput, rootfs)
	default:
		return fmt.Errorf("unsupported container type: %v", containerType)
	}
}

func createSandboxContainer(ctx context.Context, s *shimService, containerType cntr.ContainerType,
	r *taskAPI.CreateTaskRequest, ociSpec *specs.Spec, runtimeConfig *oci.RuntimeConfig,
	bundlePath, rootfsPath string, disableOutput bool, rootfs *cntr.RootFs) (err error) {
	if s.sandbox != nil {
		return fmt.Errorf("cannot create an existing sandbox: %s", s.sandbox.SandboxID())
	}

	s.config = runtimeConfig

	if containerType != cntr.PodSandbox {
		log.Debug("rootfs mounted for single container, showing rootfs contents:")
		utils.TravelDir(r.Rootfs[0].GetSource())
	}

	if errC := mountRootfs(rootfsPath, r.Rootfs); errC != nil {
		return errC
	}
	rootfs.Mounted = true

	defer func() {
		if err != nil && rootfs.Mounted {
			if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
				log.Warnf("failed to clean up rootfs mount: %v", errUmnt)
			}
		}
	}()

	if containerType != cntr.PodSandbox {
		log.Debug("rootfs mounted for single container, showing rootfs contents:")
		utils.TravelDir(rootfsPath)
	}

	var sandbox cntr.SandboxTraits
	sandbox, err = createSandbox(ctx, ociSpec, runtimeConfig, *rootfs, r.ID, bundlePath, disableOutput)
	if err != nil {
		return err
	}

	s.sandbox = sandbox
	return nil
}

func createPodContainer(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest,
	ociSpec *specs.Spec, bundlePath, rootfsPath string,
	disableOutput bool, rootfs *cntr.RootFs) (err error) {
	if s.sandbox == nil {
		return fmt.Errorf("cannot start the pod container, since the sandbox is not created")
	}

	if errC := mountRootfs(rootfsPath, r.Rootfs); errC != nil {
		return errC
	}
	rootfs.Mounted = true

	defer func() {
		if err != nil && rootfs.Mounted {
			if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
				log.Warnf("Failed to cleanup rootfs mount: %v.", errUmnt)
			}
		}
	}()

	log.Debug("rootfs mounted for pod container, showing rootfs contents: ")

	return createPodContainerInSandbox(ctx, s.sandbox, *ociSpec, *rootfs, r.ID, bundlePath, s.config, disableOutput)
}

// loadRuntimeConfig loads the runtime configuration from annotations, CRI options, or environment variables.
func loadRuntimeConfig(s *shimService, r *taskAPI.CreateTaskRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config != nil {
		return s.config, nil
	}

	stack := oci.NewRuntimeStack()

	// Config path precedence (high to low): annotations > CRI options > env.
	var (
		configPath string
		source     string
	)

	if v := oci.GetSandboxConfigPath(annotations); v != "" {
		configPath = v
		source = "annotation"
	} else if r.Options != nil {
		p, err := getConfigPathFromOptions(r.Options)
		if err != nil {
			return nil, err
		}
		if p != "" {
			configPath = p
			source = "options"
			log.Debugf("Parsed config path from options: %s.", configPath)
		}
	}

	if configPath == "" {
		if v := configstack.FirstNonEmptyEnv(defs.MicrunConfEnv); v != "" {
			configPath = v
			source = "env"
		}
	}

	if configPath != "" {
		parsed, err := loadConfigFromFile(configPath)
		if err != nil {
			if source == "env" {
				log.Warnf("Failed to load runtime config from %s (env): %v; using defaults.", configPath, err)
				stack.Replace(nil)
			} else {
				return nil, fmt.Errorf("failed to load runtime config from %s (%s): %w", configPath, source, err)
			}
		} else {
			stack.Replace(parsed)
		}
	} else {
		files, err := configstack.DiscoverMicrunConfigFiles()
		if err != nil {
			log.Warnf("micrun config discovery failed: %v", err)
		}
		stack.ApplyMicrunFiles(files)
	}

	// Apply annotations on top, as they have higher precedence.
	stack.ApplyAnnotations(annotations)
	cfg := stack.Config()
	pedestal.EnableDom0CPUExclusive(cfg.ExclusiveDom0CPU)

	s.config = cfg
	return s.config, nil
}

// getConfigPathFromOptions extracts the config path from CRI options.
func getConfigPathFromOptions(options typeurl.Any) (string, error) {
	v, err := typeurl.UnmarshalAny(options)
	if err != nil {
		return "", err
	}

	// Try current CRI options format.
	if option, ok := v.(*crioption.Options); ok {
		return option.ConfigPath, nil
	}

	// Optional backward compatibility via build tag 'oldcri'.
	if p, ok := getConfigPathFromOldCRI(v); ok {
		return p, nil
	}

	return "", nil
}

// loadConfigFromFile loads the runtime configuration from a TOML or INI file.
// BUG: Implement actual config file loading.
func loadConfigFromFile(configPath string) (*oci.RuntimeConfig, error) {
	cfg := oci.NewRuntimeConfig()
	if err := cfg.ParseRuntimeFromINI(configPath); err != nil {
		return nil, err
	}
	return cfg, nil
}

// setupMicranStateDir ensures the state directory for micran exists.
func setupMicranStateDir() error {
	if err := os.MkdirAll(defs.MicranContainerStateDir, 0755); err != nil {
		return fmt.Errorf("failed to create micran state directory: %w", err)
	}
	return nil
}

// mountRootfs mounts the container's root filesystem.
func mountRootfs(rootfsPath string, rootfs []*types.Mount) error {
	// NOTICE: Only one rootfs is supported.
	if len(rootfs) != 1 {
		log.Warnf("Only support one rootfs in bundle.")
	}

	if err := utils.MountDirs(rootfs, rootfsPath); err != nil {
		return err
	}
	return nil
}

// createSandbox initializes and creates a new sandbox instance.
func createSandbox(ctx context.Context, ocispec *specs.Spec,
	runtimeConfig *oci.RuntimeConfig, rootfs cntr.RootFs,
	containerId, bundle string, disableOutput bool) (_ cntr.SandboxTraits, err error) {

	sandboxConfig, err := oci.SandboxConfig(ocispec, *runtimeConfig, bundle, containerId, disableOutput)
	if err != nil {
		return nil, err
	}

	if !rootfs.Mounted && len(sandboxConfig.ContainerConfigs) == 1 {
		if rootfs.Source != "" {
			realPath, err := utils.ResolvePath(rootfs.Source)
			if err != nil {
				return nil, err
			}
			rootfs.Source = realPath
		}
		sandboxConfig.ContainerConfigs[containerId].Rootfs = rootfs
	}

	if err := setupNetNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			if ex := cleanupNetNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); ex != nil {
				log.Debugf("Failed to cleanup network namespace for sandbox %s: %v", sandboxConfig.ID, ex)
			}
		}
	}()

	if ocispec.Annotations == nil {
		ocispec.Annotations = make(map[string]string)
	}
	ocispec.Annotations["nerdctl/network-namespace"] = sandboxConfig.NetworkConfig.NetworkID
	sandboxConfig.Annotations["nerdctl/network-namespace"] = ocispec.Annotations["nerdctl/network-namespace"]

	// TODO: When namespace management is complete, uncomment these lines.
	// ocispec.Annotations["nerdctl/network-namespace"] = sandboxConfig.NetworkConfig.NetworkID
	// sandboxConfig.Annotations["nerdctl/network-namespace"] = ocispec.Annotations["nerdctl/network-namespace"]
	sandbox, err := cntr.CreateSandbox(ctx, &sandboxConfig)
	if err != nil {
		return nil, err
	}

	log.Debugf("Sandbox <%s> created.", sandbox.SandboxID())
	containers := sandbox.GetAllContainers()
	if len(containers) != 1 {
		return nil, fmt.Errorf("container list from sandbox is wrong, expecting only one container, got %d", len(containers))
	}

	return sandbox, nil
}

// createPodContainerInSandbox creates a container within an existing sandbox.
func createPodContainerInSandbox(ctx context.Context, sandbox cntr.SandboxTraits,
	ocispec specs.Spec, rootfs cntr.RootFs,
	containerID, bundlePath string, runtimeConfig *oci.RuntimeConfig, disableOutput bool) error {

	var defaultFirmware string
	if sandbox != nil {
		if fw, err := sandbox.Annotation(defs.FirmwarePathAnno); err == nil {
			defaultFirmware = fw
		}
	}

	containerConfig, err := oci.ParseContainerCfg(containerID, bundlePath, ocispec, cntr.PodContainer, disableOutput, defaultFirmware, runtimeConfig)
	if err != nil {
		return fmt.Errorf("failed to create container config: %w", err)
	}

	containerConfig.Rootfs = rootfs

	// Validate firmware path before creating container in sandbox
	if err := validateFirmwareForContainer(containerConfig); err != nil {
		return fmt.Errorf("firmware validation failed for container %s: %w", containerID, err)
	}

	_, err = sandbox.CreateContainer(ctx, *containerConfig)
	if err != nil {
		return fmt.Errorf("failed to create container in sandbox: %w", err)
	}

	return nil
}

func cleanupNetNS(sandboxID string, netcfg *cntr.NetworkConfig) error {
	if netcfg == nil {
		return nil
	}

	if err := netcfg.NetworkCleanup(sandboxID); err != nil {
		return fmt.Errorf("cleanup netns for sandbox %s failed: %w", sandboxID, err)
	}
	return nil
}

func setupNetNS(sandboxID string, netcfg *cntr.NetworkConfig) error {
	if netcfg == nil {
		return fmt.Errorf("setup netns: nil network config")
	}

	if netcfg.HolderPid > 0 {
		if path, err := netns.RegisterExisting(sandboxID, netcfg.HolderPid); err == nil {
			netcfg.NetworkID = path
			netcfg.NetworkCreated = true
			return nil
		}
		log.Warnf("existing netns holder pid %d for sandbox %s is invalid; recreating", netcfg.HolderPid, sandboxID)
		netcfg.HolderPid = 0
	}

	pid, path, err := netns.Create(sandboxID)
	if err != nil {
		return err
	}

	netcfg.NetworkID = path
	netcfg.NetworkCreated = true
	netcfg.HolderPid = pid
	return nil
}

func validateFirmwareForContainer(config *cntr.ContainerConfig) error {
	if config.IsInfra {
		return nil
	}

	// TODO: use multierr
	var err error
	if cntr.HostPedType == pedestal.Xen {
		if err = validate(config.PedestalConf); err != nil {
			log.Errorf("xen image file validation failed %v", err)
		}
	}
	err = validate(config.ImageAbsPath)
	if err != nil {
		return fmt.Errorf("failed to validate contaienr image files: %v", err)
	}

	return nil
}

func validate(p string) error {
	_, err := utils.EnsureRegularFilePath(p)
	return err
}

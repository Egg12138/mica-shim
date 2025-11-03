// Package shim provides the implementation of the containerd shim v2 interface for micran.
package shim

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/netns"
	"mica-shim/pkg/oci"
	"mica-shim/pkg/utils"
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

	// Create container based on its type.
	switch containerType {
	case cntr.PodSandbox, cntr.SingleContainer:
		if s.sandbox != nil {
			return nil, fmt.Errorf("cannot create an existing sandbox: %s", s.sandbox.SandboxID())
		}

		s.config = runtimeConfig
		if containerType == cntr.PodSandbox {
			s.config.SandboxCPUs, s.config.SandboxMemMB = oci.CalculateSandboxSizing(ociSpec)
		} else {
			s.config.SandboxCPUs, s.config.SandboxMemMB = oci.CalculateContainerSizing(ociSpec)
		}

		if containerType != cntr.PodSandbox {
			utils.TravelDir(r.Rootfs[0].GetSource())
		}
		if err := mountRootfs(rootfsPath, r.Rootfs); err != nil {
			return nil, err
		}
		rootfs.Mounted = true

		defer func() {
			if err != nil && rootfs.Mounted {
				if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
					log.Warnf("failed to clean up rootfs mount: %v", errUmnt)
				}
			}
		}()

		sandbox, err := createSandbox(ctx, ociSpec, runtimeConfig, rootfs, r.ID, bundlePath, disableOutput)
		if err != nil {
			return nil, err
		}

		s.sandbox = sandbox

	case cntr.PodContainer:
		if s.sandbox == nil {
			return nil, fmt.Errorf("cannot start the pod container, since the sandbox is not created")
		}

		if err = mountRootfs(rootfsPath, r.Rootfs); err != nil {
			return nil, err
		}
		rootfs.Mounted = true

		defer func() {
			if err != nil && rootfs.Mounted {
				if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
					log.Warnf("Failed to cleanup rootfs mount: %v.", errUmnt)
				}
			}
		}()

		err = createContainerInSandbox(ctx, s.sandbox, *ociSpec, rootfs, r.ID, bundlePath, disableOutput)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported container type: %v", containerType)
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

// loadRuntimeConfig loads the runtime configuration from annotations, CRI options, or environment variables.
func loadRuntimeConfig(s *shimService, r *taskAPI.CreateTaskRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config != nil {
		return s.config, nil
	}

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
		if v := os.Getenv(defs.MicranConfEnv); v != "" {
			configPath = v
			source = "env"
		}
	}

	var cfg *oci.RuntimeConfig
	if configPath != "" {
		parsed, err := loadConfigFromFile(configPath)
		if err != nil {
			if source == "env" {
				log.Warnf("Failed to load runtime config from %s (env): %v; using defaults.", configPath, err)
				cfg = oci.NewRuntimeConfig()
			} else {
				return nil, fmt.Errorf("failed to load runtime config from %s (%s): %w", configPath, source, err)
			}
		} else {
			cfg = parsed
		}
	} else {
		cfg = oci.NewRuntimeConfig()
	}

	// Apply annotations on top, as they have higher precedence.
	cfg.ParseRuntimeConfigFromAnno(annotations)

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
	if err := cfg.ParseRuntimeFromFile(configPath); err != nil {
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

	if err := setupNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); err != nil {
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
	for _, c := range containers {
		log.Debugf("Detect inside sandbox <%s>: container %s.", c.ID(), sandbox.SandboxID())
	}
	if len(containers) != 1 {
		return nil, fmt.Errorf("container list from sandbox is wrong, expecting only one container, got %d", len(containers))
	}

	return sandbox, nil
}

// createContainerInSandbox creates a container within an existing sandbox.
func createContainerInSandbox(ctx context.Context, sandbox cntr.SandboxTraits,
	ocispec specs.Spec, rootfs cntr.RootFs,
	containerID, bundlePath string, disableOutput bool) error {

	var defaultFirmware string
	if sandbox != nil {
		if fw, err := sandbox.Annotation(defs.FirmwarePath); err == nil {
			defaultFirmware = fw
		}
	}

	containerConfig, err := oci.ContainerConfig(containerID, bundlePath, ocispec, cntr.PodContainer, disableOutput, defaultFirmware)
	if err != nil {
		return fmt.Errorf("failed to create container config: %w", err)
	}

	containerConfig.Rootfs = rootfs

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

func setupNS(sandboxID string, netcfg *cntr.NetworkConfig) error {
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

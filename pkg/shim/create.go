package shim

import (
	"context"
	"errors"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/fileutils"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/oci"
	"os"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go"

	// only register the proto type
	crioption "github.com/containerd/containerd/pkg/runtimeoptions/v1"
)

func create(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest) (*container, error) {
	err := setupMicranStateDir()
	if err != nil {
		log.Debugf("failed to setup micran state directory: %w", err)
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

	// Create container based on type
	switch containerType {
	case cntr.PodSandbox, cntr.SingleContainer:
		// Handle sandbox creation logic
		// return createSandboxContainer(ctx, s, r, &ociSpec, containerType, runtimeConfig, rootfs, rootfsPath, disableOutput)
		if s.sandbox != nil {
			return nil, fmt.Errorf("cannot create an existing sandbox: %s", s.sandbox.SandboxID())
		}

		s.config = runtimeConfig
		if containerType == cntr.PodSandbox {
			s.config.SandboxCPUs, s.config.SandboxMemMB = oci.CalculateSandboxSizing(ociSpec)
		} else {
			s.config.SandboxCPUs, s.config.SandboxMemMB = oci.CalculateContainerSizing(ociSpec)
		}

		if err := mountRootfs(rootfsPath, r.Rootfs); err != nil {
			return nil, err
		}

		_ = fileutils.Backup(r.Bundle)
		rootfs.Mounted = true

		defer func() {
			if err != nil && rootfs.Mounted {
				if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
					log.Warnf("failed to cleanup rootfs mount: %v", errUmnt)
				}
			}
		}()

		sandbox, err := createSandbox(ctx, ociSpec, runtimeConfig, rootfs, r.ID, bundlePath, disableOutput)
		if err != nil {
			return nil, err
		}

		s.sandbox = sandbox
		s.shimPid = uint32(os.Getpid())

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
					log.Warnf("failed to cleanup rootfs mount: %v", errUmnt)
				}
			}
		}()

		err = createContainer(ctx, s.sandbox, *ociSpec, rootfs, r.ID, bundlePath, disableOutput)
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
	return container, nil

}

func loadRuntimeConfig(s *shimService, r *taskAPI.CreateTaskRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	if s.config != nil {
		return s.config, nil
	}

	configPath := oci.GetSandboxConfigPath(annotations)

	if configPath == "" && r.Options != nil {
		var err error
		configPath, err = getConfigPathFromOptions(r.Options)
		if err != nil {
			return nil, err
		}
		log.Debugf("parsed config path from options: %s", configPath)
	}

	// Try to get the config file from the environment
	if configPath == "" {
		configPath = os.Getenv(defs.MicranConfEnv)
	}

	// set configPath through environment variable
	if configPath != "" {
		if _, err := loadConfigFromFile(configPath); errors.Is(err, errdefs.ErrNotImplemented) {
			log.Warnf("loading config from file is not implemented yet")
		}
	}

	if s.config == nil {
		s.config = oci.NewRuntimeConfig()
	}

	// Default: parse runtime config from annotations
	return parseRuntimeConfigFromAnnotations(annotations), nil
}

func getConfigPathFromOptions(options typeurl.Any) (string, error) {
	v, err := typeurl.UnmarshalAny(options)
	if err != nil {
		return "", err
	}

	// Try CRI options format
	option, ok := v.(*crioption.Options)
	if ok {
		return option.ConfigPath, nil
	}

	return "", nil
}

// toml or ini
// TODO: Implement actual config file loading
func loadConfigFromFile(configPath string) (*oci.RuntimeConfig, error) {
	// For now, create default config and enhance with file-specific settings
	config := oci.NewRuntimeConfig()
	return config, errdefs.ErrNotImplemented
}

func parseRuntimeConfigFromAnnotations(annotations map[string]string) *oci.RuntimeConfig {
	rc := oci.NewRuntimeConfig()
	return rc.ParseRuntimeConfig(annotations)
}

// loadContainerState loads container state from various locations
// TODO: Implement proper state loading logic
func loadContainerState(id string) (*cntr.ContainerState, error) {
	// Placeholder implementation - this needs to be implemented based on
	// the actual state persistence mechanism used in micran
	return nil, fmt.Errorf("state loading not implemented")
}

func setupMicranStateDir() error {
	if err := os.MkdirAll(defs.MicranStateDir, 0755); err != nil {
		return fmt.Errorf("failed to create micran state directory: %w", err)
	}
	return nil
}

func saveContainerState(c *cntr.Container) error {
	if err := os.MkdirAll(filepath.Join(defs.MicranStateDir, c.ID()), 0o755); err != nil {
		log.Debugf("failed to create <%s> state directory: %v", c.ID)
		return err
	}
	return c.SaveState()
}

func mountRootfs(rootfsPath string, rootfs []*types.Mount) error {
	// NOTICE: only support one rootfs!
	if len(rootfs) != 1 {
		log.Warnf("only support one rootfs in bundle")
	}

	if err := fileutils.MountDirs(rootfs, rootfsPath); err != nil {
		return err
	}
	return nil
}

func createSandbox(ctx context.Context, ocispec *specs.Spec,
	runtimeConfig *oci.RuntimeConfig, rootfs cntr.RootFs,
	containerId, bundle string, disableOutput bool) (_ cntr.SandboxTraits, err error) {

	sandboxConfig, err := oci.SandboxConfig(ocispec, *runtimeConfig, bundle, containerId, disableOutput)
	if err != nil {
		return nil, err
	}

	if !rootfs.Mounted && len(sandboxConfig.ContainerConfigs) == 1 {
		if rootfs.Source != "" {
			realPath, err := fileutils.ResolvePath(rootfs.Source)
			if err != nil {
				return nil, err
			}
			rootfs.Source = realPath
		}
		sandboxConfig.ContainerConfigs[containerId].Rootfs = rootfs
	}

	if err := setupNS(&sandboxConfig.NetworkConfig); err != nil {
		return nil, err
	}

	defer func() {
		ns := sandboxConfig.NetworkConfig
		if err != nil && ns.NetworkCreated {
			if ex := cleanupNetNS(ns.NetworkID); ex != nil {
				log.Debugf("failed to cleanup network namespace %s", ns.NetworkID)
			}
		}
	}()

	if ocispec.Annotations == nil {
		ocispec.Annotations = make(map[string]string)
	}
	ocispec.Annotations["nerdctl/network-namespace"] = sandboxConfig.NetworkConfig.NetworkID
	sandboxConfig.Annotations["nerdctl/network-namespace"] = ocispec.Annotations["nerdctl/network-namespace"]

	// TODO: when namespace management is over, uncomment these:
	// ocispec.Annotations["nerdctl/network-namespace"] = sandboxConfig.NetworkConfig.NetworkID
	// sandboxConfig.Annotations["nerdctl/network-namespace"] = ocispec.Annotations["nerdctl/network-namespace"]
	sandbox, err := cntr.CreateSandbox(ctx, &sandboxConfig)
	if err != nil {
		return nil, err
	}

	log.Debugf("sandbox <%s> created", sandbox.SandboxID())
	containers := sandbox.GetAllContainers()
	log.Debugf("containers inside sandbox: %v", containers)
	for _, c := range containers {
		log.Debugf("container <%s> inside sandbox <%s>", c.ID(), sandbox.SandboxID())
	}
	if len(containers) != 1 {
		return nil, fmt.Errorf("container list from sandbox is wrong, expecting only one container, got %d", len(containers))
	}

	return sandbox, nil

}

func createContainer(ctx context.Context, sandbox cntr.SandboxTraits,
	ocispec specs.Spec, rootfs cntr.RootFs,
	containerID, bundlePath string, disableOutput bool) error {

	containerConfig, err := oci.ContainerConfig(containerID, bundlePath, ocispec, cntr.PodContainer, disableOutput)
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

func cleanupNetNS(netns string) error {
	return nil
}

func setupNS(netcfg *cntr.NetworkConfig) error {
	return nil
}

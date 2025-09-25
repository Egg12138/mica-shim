package shim

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/oci"
	"mica-shim/pkg/utils"
	"os"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go"

	// only register the proto type
	crioption "github.com/containerd/containerd/pkg/runtimeoptions/v1"
)

func create(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest) (_ *container, err error) {
	err = setupMicranStateDir()
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

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config != nil {
		return s.config, nil
	}

	// Config path precedence (high -> low): annotations > CRI options > env
	var (
		configPath string
		source     string // "annotation" | "options" | "env"
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
			log.Debugf("parsed config path from options: %s", configPath)
		}
	}

	if configPath == "" {
		if v := os.Getenv(defs.MicranConfEnv); v != "" {
			configPath = v
			source = "env"
		}
	}

	// Build base config from file if provided. Fail-fast if user explicitly provided
	// path via annotations or CRI options. Env-provided path failure falls back.
	var cfg *oci.RuntimeConfig
	if configPath != "" {
		parsed, err := loadConfigFromFile(configPath)
		if err != nil {
			if source == "env" {
				log.Warnf("failed to load runtime config from %s (env): %v; using defaults", configPath, err)
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

	// Apply annotations on top (higher precedence overrides)
	cfg.ParseRuntimeConfigFromAnno(annotations)

	s.config = cfg
	return s.config, nil
}

func getConfigPathFromOptions(options typeurl.Any) (string, error) {
	v, err := typeurl.UnmarshalAny(options)
	if err != nil {
		return "", err
	}

	// Try current CRI options format
	if option, ok := v.(*crioption.Options); ok {
		return option.ConfigPath, nil
	}

	// Optional backward compatibility via build tag 'oldcri'
	if p, ok := getConfigPathFromOldCRI(v); ok {
		return p, nil
	}

	return "", nil
}

// toml or ini
// BUG: Implement actual config file loading
func loadConfigFromFile(configPath string) (*oci.RuntimeConfig, error) {
	cfg := oci.NewRuntimeConfig()
	if err := cfg.ParseRuntimeFromFile(configPath); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseRuntimeConfigFromAnnotations(annotations map[string]string) *oci.RuntimeConfig {
	rc := oci.NewRuntimeConfig()
	return rc.ParseRuntimeConfigFromAnno(annotations)
}

// loadContainerState loads container state from various locations
// TODO: Implement proper state loading logic
func loadContainerState(id string) (*cntr.ContainerState, error) {
	// Placeholder implementation - this needs to be implemented based on
	// the actual state persistence mechanism used in micran
	return nil, fmt.Errorf("state loading not implemented")
}

func setupMicranStateDir() error {
	if err := os.MkdirAll(defs.DefaultMicranStateDir, 0755); err != nil {
		return fmt.Errorf("failed to create micran state directory: %w", err)
	}
	return nil
}

func saveContainerState(c *cntr.Container) error {
	if err := os.MkdirAll(filepath.Join(defs.DefaultMicranStateDir, c.ID()), 0o755); err != nil {
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

	if err := utils.MountDirs(rootfs, rootfsPath); err != nil {
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
			realPath, err := utils.ResolvePath(rootfs.Source)
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
	for _, c := range containers {
		log.Infof("detect inside sandbox <%s>: container %s", c.ID(), sandbox.SandboxID())
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

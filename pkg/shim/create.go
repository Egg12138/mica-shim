package shim

import (
	"context"
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
	ociSpec, err := oci.LoadSpec(r.Bundle)
	if err != nil {
		return nil, err
	}

	containerType, err := oci.GetContainerType(&ociSpec)
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
		return createSandboxContainer(ctx, s, r, &ociSpec, containerType, runtimeConfig, rootfs, rootfsPath, disableOutput)
	case cntr.PodContainer:
		// Handle container creation within existing sandbox
		return createPodContainer(ctx, s, r, &ociSpec, containerType, runtimeConfig, rootfs, rootfsPath, disableOutput)
	default:
		return nil, fmt.Errorf("unsupported container type: %v", containerType)
	}
}

func loadRuntimeConfig(s *shimService, r *taskAPI.CreateTaskRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	if s.config != nil {
		return s.config, nil
	}

	// Config override ordering (high to low):
	// 1. podsandbox annotation
	// 2. shimv2 create task option  
	// 3. environment
	// 4. default from annotations parsing
	configPath := oci.GetSandboxConfigPath(annotations)
	
	if configPath == "" && r.Options != nil {
		var err error
		configPath, err = getConfigPathFromOptions(r.Options)
		if err != nil {
			return nil, err
		}
	}

	// Try to get the config file from the environment
	if configPath == "" {
		configPath = os.Getenv("MICRAN_CONF_FILE")
	}

	// Load config from file if specified, otherwise parse from annotations
	if configPath != "" {
		return loadConfigFromFile(configPath)
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
	if option, ok := v.(*crioption.Options); ok {
		return option.ConfigPath, nil
	}

	return "", nil
}

func loadConfigFromFile(configPath string) (*oci.RuntimeConfig, error) {
	// TODO: Implement actual config file loading
	// For now, create default config and enhance with file-specific settings
	config := oci.NewRuntimeSpec()
	
	// Here you would parse the config file and override defaults
	// config.SetDebug(true)
	// config.SetSandboxCPUs(4)
	// etc.
	
	return config, nil
}

func parseRuntimeConfigFromAnnotations(annotations map[string]string) *oci.RuntimeConfig {
	return oci.ParseRuntimeConfig(annotations)
}

func createSandboxContainer(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest, 
	ociSpec *specs.Spec, containerType cntr.ContainerType, runtimeConfig *oci.RuntimeConfig, 
	rootfs cntr.RootFs, rootfsPath string, disableOutput bool) (*cntr.Container, error) {
	
	// TODO: Implement sandbox creation logic
	// This would involve:
	// 1. Setting up the sandbox environment
	// 2. Allocating CPU cores for RTOS
	// 3. Initializing communication channels
	// 4. Creating the container structure
	
	if s.sandbox != nil {
		return nil, fmt.Errorf("cannot create an existing sandbox: %s", s.sandbox.SandboxID())
	}

	s.config = runtimeConfig

	if containerType == cntr.PodSandbox {
		s.config.SandboxCPUs, s.config.SandboxMemMB = oci.CalculateSandboxSizing(ociSpec)
	} else {
		s.config.SandboxCPUs, s.config.SandboxMemMB = oci.CalculateContainerSizing(ociSpec)
	}

	var err error
	if err = mountRootfs(r.Bundle, r.Rootfs); err != nil {
		return nil, err
	}
	rootfs.Mounted = true

	defer func() {
		if err != nil && rootfs.Mounted {
			if errMnt := mount.UnmountAll(rootfsPath, 0); errMnt != nil {
				log.Warnf("failed to cleanup rootfs mount: %w", errMnt)
			}
		}
	}()

	sandboxConfig, err := oci.SandboxConfig(ociSpec, *runtimeConfig, r.Bundle, r.ID, disableOutput)
	if err != nil {
		log.Debugf("Failed to generate sandbox config from runtime config: %v", runtimeConfig)
		return nil, err
	}
	sandbox, err := cntr.CreateSandbox(s.ctx, sandboxConfig)
	
	
	return container, nil
}

func createPodContainer(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest, 
	ociSpec *specs.Spec, containerType cntr.ContainerType, runtimeConfig *oci.RuntimeConfig, 
	rootfs cntr.RootFs, rootfsPath string, disableOutput bool) (*cntr.Container, error) {
	
	// TODO: Implement pod container creation logic
	// This would involve:
	// 1. Verifying sandbox exists
	// 2. Creating container within existing sandbox
	// 3. Setting up container-specific resources
	
	// Create container config first
	config := &cntr.ContainerConfig{
		ID:             r.ID,
		Rootfs:         rootfs,
		Annotations:    ociSpec.Annotations,
		Pid:            os.Getpid(),
		// Set other config fields from annotations and OCI spec
	}
	
	// Create container - TODO: Use proper constructor
	container := &cntr.Container{
		// Container has private fields, so we need to use a proper constructor
		// This is a placeholder until we implement the proper creation logic
	}
	
	return container, nil
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


func mountRootfs(bundle string, rootfs []*types.Mount) error { 
	// NOTICE: only support one rootfs!
	if len(rootfs) != 1 {
		return fmt.Errorf("only support one rootfs")
	}

	rootfsPath := filepath.Join(bundle, "rootfs")
	if err := fileutils.MountDirs(rootfs, rootfsPath); err != nil {
		return err
	}
	return  nil
}

func createSandbox(ctx context.Context, sb cntr.MicantainerManager, ocispec *specs.Spec, 
	 runtimeConfig *oci.RuntimeConfig, rootfs cntr.RootFs, 
	 containerId, bundle string, disableOutput bool) (_ cntr.SandboxTraits, err error) {

		sandboxConfig, err := cntr.DummySandboxConfig(containerId, ocispec)
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

		// TODO: when namespace management is over, uncomment these:
		// ocispec.Annotations["nerdctl/network-namespace"] = sandboxConfig.NetworkConfig.NetworkID
		// sandboxConfig.Annotations["nerdctl/network-namespace"] = ocispec.Annotations["nerdctl/network-namespace"]

		sandbox, err := cntr.CreateSandbox(ctx, sandboxConfig)
		if err != nil {
			return nil, err
		}

		log.Debugf("sandbox <%s> created", sandbox.SandboxID)
		containers := sandbox.GetAllContainers()
		log.Debugf("containers inside sandbox: %v", containers)
		if len(containers) != 1 {
			return nil, fmt.Errorf("Container list from sandbox is wrong, expecting only one container, got %d", len(containers))
		}

		return sandbox, nil
}

func cleanupNetNS(netns string) error {
	return nil
}

func setupNS(netcfg *cntr.NetworkConfig) error {
	return nil
}
package oci

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	defs "mica-shim/definitions"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/pedestal"
	"mica-shim/pkg/utils"

	ctrAnnotations "github.com/containerd/containerd/pkg/cri/annotations"
	podmanAnnotations "github.com/containers/podman/v4/pkg/annotations"

	// TODO: remove dockershim annotation
	"github.com/opencontainers/runtime-spec/specs-go"
)

type annotationContainerType struct {
	annotation    string
	containerType cntr.ContainerType
}

// CRI types list reference: kata-containers.
var (
	// CRIContainerTypeKeyList lists all the CRI keys that could define
	// the container type from annotations in the config.json.
	CRIContainerTypeKeyList = []string{ctrAnnotations.ContainerType, podmanAnnotations.ContainerType}

	// CRISandboxNameKeyList lists all the CRI keys that could define
	// the sandbox ID from annotations in the config.json.
	CRISandboxNameKeyList = []string{ctrAnnotations.SandboxID, podmanAnnotations.SandboxID}

	// CRIContainerTypeList lists all the maps from CRI ContainerTypes annotations
	// to a virtcontainers ContainerType.
	CRIContainerTypeList = []annotationContainerType{
		{podmanAnnotations.ContainerTypeSandbox, cntr.PodSandbox},
		{podmanAnnotations.ContainerTypeContainer, cntr.PodContainer},
		{ctrAnnotations.ContainerTypeSandbox, cntr.PodSandbox},
		{ctrAnnotations.ContainerTypeContainer, cntr.PodContainer},
	}
)

func GetContainerType(spec *specs.Spec) (cntr.ContainerType, error) {
	for _, key := range CRIContainerTypeKeyList {
		containerType, ok := spec.Annotations[key]
		if !ok {
			continue
		}

		for _, t := range CRIContainerTypeList {
			if t.annotation == containerType {
				return t.containerType, nil
			}
		}
		return cntr.UnknownContainerType, fmt.Errorf("unknown container type: %s", containerType)
	}
	return cntr.SingleContainer, nil
}

func GetSandboxID(spec *specs.Spec) (string, error) {
	for _, key := range CRISandboxNameKeyList {
		sandboxID, ok := spec.Annotations[key]
		if ok {
			return sandboxID, nil
		}
	}
	return "", fmt.Errorf("sandbox ID not found in annotations")
}

func GetSandboxConfigPath(annotations map[string]string) string {
	return annotations[defs.SandboxConfigPathKey]
}

func bundleRootfs(bundle string) string {
	return filepath.Join(bundle, "rootfs")
}



func ContainerConfig(id, bundle string, ocispec specs.Spec, Type cntr.ContainerType, detach bool) (*cntr.ContainerConfig, error) {
	configPath := filepath.Join(bundleRootfs(bundle), defs.DefaultClientConf)
	log.Debugf("config path = %s", configPath)
	micaConf, err := utils.ParseConfigINI(configPath, defs.OKSectionList[:])
	log.Pretty("mica config: %v", micaConf)
	
	// Debug: Check if file exists and list all parsed keys
	if _, err := os.Stat(configPath); err == nil {
		log.Debugf("Parsed %d keys from client.conf:", len(micaConf))
		for k, v := range micaConf {
			log.Debugf("  '%s' = '%s'", k, v)
		}
	} else {
		log.Debugf("%s file does not exist at: %s", defs.DefaultClientConf, configPath)
	}
	if err != nil {
		return nil, err
	}

	pedtype := cntr.HostPedType
	if pedAnnotation, ok := ocispec.Annotations[defs.Pedtype]; ok {
		parsedType := pedestal.ParsePedType(pedAnnotation)
		if parsedType != pedestal.Unsupported {
			pedtype = parsedType
			log.Debugf("found pedestal type annotation: %s", pedAnnotation)
		} else {
			log.Warnf("unknown pedestal type '%s', using default", pedAnnotation)
		}
	}

	var pedconf string
	if pedtype == pedestal.Xen {
		if cfg, ok := micaConf[defs.PedCfg]; ok && cfg != "" {
			pedconf = cfg
			log.Debugf("pedestal config for xen is the location of <image.bin>: %s", pedconf)
		} else {
			log.Debugf("use default pedestal config for xen <image.bin>: %s", pedconf)
			pedconf = pedestal.XenDefaultPedConf()
		}
	}

	// Read OS from annotation
	osName := "zephyr" // default
	if osAnnotation, ok := ocispec.Annotations[defs.OSAnnotation]; ok {
		osName = osAnnotation
		log.Debugf("found OS annotation: %s", osName)
	}

	// Debug: Log the parsed mica configuration
	log.Debugf("parsed micaConf: %+v", micaConf)
	log.Debugf("looking for clientpath key '%s', found value: '%s'", defs.ElfPath, micaConf[defs.ElfPath])

	// Validate ElfPath - critical for RTOS execution
	elfPath := micaConf[defs.ElfPath]
	if elfPath == "" {
		// Try default paths
		defaultElfPath := filepath.Join(bundleRootfs(bundle), "zephyr.elf")
		if _, err := os.Stat(defaultElfPath); err == nil {
			elfPath = defaultElfPath
			log.Debugf("using default elf path: %s", elfPath)
		} else {
			// Last resort - look for any .elf file in rootfs
			elfFiles, _ := filepath.Glob(filepath.Join(bundleRootfs(bundle), "*.elf"))
			if len(elfFiles) > 0 {
				elfPath = elfFiles[0]
				log.Debugf("found elf file: %s", elfPath)
			} else {
				return nil, fmt.Errorf("no elf file found in container rootfs and no clientpath specified in %s", defs.DefaultClientConf)
			}
		}
	}

	// init
	config := &cntr.ContainerConfig{
		// Container ID
		ID:           id,
		// OCI and bundle info
		ElfPath:      elfPath,
		PedestalType: pedtype,
		PedestalConf: pedconf,
		OS:           osName,
		PCPUNum:         1,
		CpuLimit:     0,
		CpusetCpus:   "",
		CpuShares:    0,
		CpuQuota:     0,
		CpuPeriod:    0,

		// Memory defaults
		MemoryLimitMB:       0,
		MemoryReservationMB: 0,
		MemorySwapMB:        0,
		MemoryKernelMB:      0,
		MemorySwappinessMB:  nil,
		OomKillDisable:    false,
	}

	// TODO: remove the duplicated parsing
	if err := config.ParseOCICPUResources(&ocispec); err != nil {
		return nil, err
	}

	if err := config.ParseOCIMemoryResources(&ocispec); err != nil {
		return nil, err
	}

	// Validate resource limits against system constraints
	if err := cntr.ValidateResourceLimits(config); err != nil {
		log.Warnf("resource validation warning: %v", err)
		// Don't fail the container creation for resource validation warnings
		// but log them for visibility
	}

	// OS is already set from annotation or default above
	log.Infof("container OS: %s", config.OS)

	log.Infof("container resource limits - CPU: %s, Memory: %s",
		formatCPULimit(config), formatMemoryLimit(config))
	return config, nil
}

func SandboxConfig(ocispec *specs.Spec, rc RuntimeConfig, bundle, sbContainerID string, detach bool) (cntr.SandboxConfig, error) {
	// generate sandbox container config
	containerConfig, err := ContainerConfig(sbContainerID, bundle, *ocispec, cntr.PodSandbox, detach)
	if err != nil {
		return cntr.SandboxConfig{}, err
	}
	// TODO: allocated shared resources

	networkConfig := cntr.NetworkConfig{}
	ped := cntr.HostPedType
	if ped == pedestal.Xen {
		pedcfg := filepath.Join(bundleRootfs(bundle), defs.DefaultXenBin)
		log.Debugf("pedestal config for xen is the location of <image.bin>: %s", pedcfg)
	}

	staticResMngt := rc.StaticResourceManagement
	hugePage := pedestal.HugePageSupport(staticResMngt)


	// update container resource for openamp-based client is out of plan
	
	if pedestal.GetHostPed() == pedestal.OpenAMP {
		staticResMngt = true
	}

	sandboxConfig := cntr.SandboxConfig{
		ID:       sbContainerID,
		Hostname: ocispec.Hostname,
		PedConfig:  pedestal.PedestalConfig{
			PedType:       pedestal.GetHostPed(),
			PedConfig: "",
			MiniVCPUNum:   rc.MiniVCPUNum,
		},
		ContainerConfigs: map[string]*cntr.ContainerConfig{
			sbContainerID: containerConfig,
		},
		NetworkConfig: networkConfig,
		Annotations: map[string]string{
			defs.BundlePathKey: bundle,
		},
		SandboxResources: cntr.SandboxResourceSizing{
			WorkloadCPUs:  rc.SandboxCPUs,
			WorkloadMemMB: rc.SandboxMemMB,
		},

		StaticResourceMgmt: staticResMngt,
		HugePageSupport: hugePage,
		EnableVCPUsPining: false,
	}


	applySandboxAnnotations(*ocispec, &sandboxConfig)
	return sandboxConfig, nil
}

// formatCPULimit formats CPU limit information into human readable string
func formatCPULimit(config *cntr.ContainerConfig) string {
	if config == nil {
		return "unlimited"
	}

	parts := []string{}

	if config.CpuLimit > 0 {
		parts = append(parts, fmt.Sprintf("limit=%d cores", config.CpuLimit))
	}

	if config.CpuQuota > 0 && config.CpuPeriod > 0 {
		ratio := float64(config.CpuQuota) / float64(config.CpuPeriod)
		parts = append(parts, fmt.Sprintf("quota=%.2f cores", ratio))
	}

	if config.CpuShares > 0 {
		parts = append(parts, fmt.Sprintf("shares=%d", config.CpuShares))
	}

	if config.CpusetCpus != "" {
		parts = append(parts, fmt.Sprintf("cpuset=%s", config.CpusetCpus))
	}

	if len(parts) == 0 {
		return "unlimited"
	}

	return strings.Join(parts, ", ")
}

// formatMemoryLimit formats memory limit information into human readable string
func formatMemoryLimit(config *cntr.ContainerConfig) string {
	if config == nil {
		return "unlimited"
	}

	parts := []string{}

	if config.MemoryLimitMB > 0 {
		parts = append(parts, fmt.Sprintf("limit=%s", formatBytes(int64(config.MemoryLimitMB)*1024*1024)))
	}

	if config.MemoryReservationMB > 0 {
		parts = append(parts, fmt.Sprintf("reservation=%s", formatBytes(int64(config.MemoryReservationMB)*1024*1024)))
	}

	if config.MemorySwapMB > 0 {
		parts = append(parts, fmt.Sprintf("swap=%s", formatBytes(int64(config.MemorySwapMB)*1024*1024)))
	}

	if config.MemoryKernelMB > 0 {
		parts = append(parts, fmt.Sprintf("kernel=%s", formatBytes(int64(config.MemoryKernelMB)*1024*1024)))
	}

	if config.MemorySwappinessMB != nil {
		parts = append(parts, fmt.Sprintf("swappiness=%d", *config.MemorySwappinessMB))
	}

	if config.OomKillDisable {
		parts = append(parts, "oom-kill=disabled")
	}

	if len(parts) == 0 {
		return "unlimited"
	}

	return strings.Join(parts, ", ")
}

// formatBytes formats bytes into human readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func applySandboxAnnotations(ocispec specs.Spec, cfg *cntr.SandboxConfig) {
	if ocispec.Annotations == nil || cfg == nil {
		return
	}
	if cfg.Annotations == nil {
		cfg.Annotations = make(map[string]string)
	}

	for key, value := range ocispec.Annotations {
		if !strings.HasPrefix(key, defs.MicraAnnotationPrefix) || value == "" {
			continue
		}
		switch key {
		// allowlist: only handle known, safe sandbox-level toggles
		case defs.RuntimePrefix + "enable_vcpus_pinning":
			if b, err := strconv.ParseBool(value); err == nil {
				cfg.EnableVCPUsPining = b
			} else {
				log.Debugf("invalid bool for %s: %s", key, value)
			}
			cfg.Annotations[key] = value

		case defs.RuntimePrefix + "static_resource":
			if b, err := strconv.ParseBool(value); err == nil {
				cfg.StaticResourceMgmt = b
			} else {
				log.Debugf("invalid bool for %s: %s", key, value)
			}
			cfg.Annotations[key] = value

		case defs.RuntimePrefix + "hugepage_enable":
			if b, err := strconv.ParseBool(value); err == nil {
				cfg.HugePageSupport = b
			} else {
				log.Debugf("invalid bool for %s: %s", key, value)
			}
			cfg.Annotations[key] = value

		default:
			// ignore other annotations at sandbox level for now
		}
	}
}

func GetContainerSpec(annotations map[string]string) (specs.Spec, error) {
	if bundlePath, ok := annotations[defs.BundlePathKey]; ok {
		return parseConfigJSON(bundlePath)
	}

	log.Debugf("annotations[%s] not found, cannot find container spec",
		defs.BundlePathKey)
	return specs.Spec{}, fmt.Errorf("Could not find container spec")
}

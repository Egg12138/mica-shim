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

func ContainerConfig(id, bundle string, ocispec specs.Spec, Type cntr.ContainerType, detach bool, defaultFirmwarePath string) (*cntr.ContainerConfig, error) {
	baseRoot := bundleRootfs(bundle)

	getAnnotation := func(key string ) (string, bool) {
		if ocispec.Annotations == nil {
			return "", false
		}
		if raw, ok := ocispec.Annotations[key]; ok {
			trimmed := strings.TrimSpace(raw)
			if trimmed != "" {
				return trimmed, true
			}
		}
		return "", false
	}

	pedtype := cntr.HostPedType
	if pedAnnotation, ok := getAnnotation(defs.Pedtype); ok {
		parsedType := pedestal.ParsePedType(pedAnnotation)
		if parsedType != pedestal.Unsupported {
			pedtype = parsedType
			log.Debugf("found pedestal type annotation: %s", pedAnnotation)
		} else {
			log.Warnf("unknown pedestal type '%s', using default", pedAnnotation)
		}
	}

	var pedconf string
	hasPedestalConfAnnotation := false
	if pedtype == pedestal.Xen {
		if cfg, ok := getAnnotation(defs.PedestalConf); ok {
			pedconf = cfg
			hasPedestalConfAnnotation = true
			log.Debugf("pedestal config from annotation: %s", pedconf)
		}
		if pedconf == "" {
			pedconf = pedestal.XenDefaultPedConf()
			log.Debugf("using default pedestal config for xen <image.bin>: %s", pedconf)
		}
		// Make pedcfg absolute within bundle rootfs if necessary
		if pedconf != "" && !filepath.IsAbs(pedconf) {
			pedconf = filepath.Join(baseRoot, pedconf)
			log.Debugf("===>>pedestal conf path= %s", pedconf)
		}
	}

	// Read OS from annotation
	osName := "zephyr" // default
	if osAnnotation, ok := getAnnotation(defs.OSAnnotation); ok {
		osName = osAnnotation
		log.Debugf("found OS annotation: %s", osName)
	}

	resolveFirmwarePath := func(p string, strict bool) (string, error) {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return "", nil
		}

		// Candidate search order:
		//   1) host-absolute path (allows runtime config defaults to be absolute)
		//   2) bundle rootfs relative path (supports annotations that provide container paths)
		var candidates []string
		if filepath.IsAbs(trimmed) {
			candidates = append(candidates, trimmed)
			rel := strings.TrimPrefix(trimmed, string(filepath.Separator))
			if rel != "" {
				candidates = append(candidates, filepath.Join(baseRoot, rel))
			}
		} else {
			candidates = append(candidates, filepath.Join(baseRoot, trimmed))
		}

		var lastErr error
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			info, err := os.Stat(candidate)
			if err != nil {
				if !os.IsNotExist(err) {
					return "", fmt.Errorf("firmware path %s lookup failed: %w", candidate, err)
				}
				lastErr = err
				continue
			}
			if info.IsDir() {
				lastErr = fmt.Errorf("path %s is a directory", candidate)
				continue
			}

			resolved, err := utils.ResolvePath(candidate)
			if err != nil {
				return "", err
			}
			return resolved, nil
		}

		if strict {
			return "", fmt.Errorf("firmware path %s is invalid: %w", trimmed, lastErr)
		}
		log.Debugf("firmware path %s not found, falling back to defaults", trimmed)
		return "", nil
	}

	annotationFirmware := ""
	if fw, ok := getAnnotation(defs.FirmwarePath); ok {
		annotationFirmware = fw
	}

	hasMicranAnnotation := false
	log.Pretty("spec annotations=%v", ocispec.Annotations)
	if ocispec.Annotations != nil {
		for key := range ocispec.Annotations {
			if strings.HasPrefix(key, defs.MicraAnnotationPrefix) {
				hasMicranAnnotation = true
				break
			}
		}
	}

	isInfraCandidate := false
	if ocispec.Linux != nil {
		for _, ns := range ocispec.Linux.Namespaces {
			if ns.Type == specs.NetworkNamespace {
				isInfraCandidate = ns.Path == ""
				break
			}
		}
	}

	isCRIInfra := Type == cntr.PodSandbox
	log.Debugf("isCRIInfra=%v, isInfraCandidate=%v, hasMicranAnnotation=%v, hasPedestalConfAnnotation=%v, annotationFirmware=%s", isCRIInfra, isInfraCandidate, hasMicranAnnotation, hasPedestalConfAnnotation, annotationFirmware)
	infraEligible := annotationFirmware == "" && !hasMicranAnnotation && !hasPedestalConfAnnotation

	isInfra := false
	switch {
	case isCRIInfra && infraEligible:
		isInfra = true
	case infraEligible && isInfraCandidate:
		// fallback: allow empty-netns inference for ctr/nerdctl flows.
		isInfra = true
	}

	// Resolve firmware path priority: annotation > runtime default > discovery
	var elfPath string
	if !isInfra {
		switch {
		case annotationFirmware != "":
			if resolved, err := resolveFirmwarePath(annotationFirmware, true); err != nil {
				return nil, fmt.Errorf("failed to resolve firmware path from annotation: %w", err)
			} else {
				elfPath = resolved
			}
		case strings.TrimSpace(defaultFirmwarePath) != "":
			if resolved, err := resolveFirmwarePath(defaultFirmwarePath, false); err != nil {
				return nil, fmt.Errorf("failed to resolve firmware path from runtime config: %w", err)
			} else {
				elfPath = resolved
			}
		default:
			// No explicit firmware path provided; fall back to common defaults.
		}

    log.Debugf("===>> elf path = %s", elfPath)
		if elfPath == "" {
			defaultElfPath := filepath.Join(baseRoot, defs.DefaultFirmwareName)
			if _, err := os.Stat(defaultElfPath); err == nil {
				elfPath = defaultElfPath
				log.Debugf("using default elf path: %s", elfPath)
			} else {
				elfFiles, _ := filepath.Glob(filepath.Join(baseRoot, "*.elf"))
				if len(elfFiles) > 0 {
					elfPath = elfFiles[0]
					log.Debugf("found elf file: %s", elfPath)
				} else {
					return nil, fmt.Errorf("no elf file found in container rootfs and no firmware path provided via annotation or runtime configuration")
				}
			}
		}
	}

	// init
	config := &cntr.ContainerConfig{
		// Container ID
		ID: id,
		// OCI and bundle info
		ElfAbsPath:      elfPath,
		PedestalType: pedtype,
		PedestalConf: pedconf,
		OS:           osName,
		PCPUNum:      1,
		CpuLimit:     0,
		CpusetCpus:   "",
		CpuShares:    0,
		CpuQuota:     0,
		CpuPeriod:    0,

		// Memory defaults
		MemoryLimitMB:       0,
		MemoryMinMB:         0,
		MemoryReservationMB: 0,
		MemorySwapMB:        0,
		MemoryKernelMB:      0,
		MemorySwappinessMB:  nil,
		OomKillDisable:      false,
	}
	config.IsInfra = isInfra

	// TODO: remove the duplicated parsing
	if err := config.ParseOCICPUResources(&ocispec); err != nil {
		return nil, err
	}

	if err := config.ParseOCIMemoryResources(&ocispec); err != nil {
		return nil, err
	}

	// Container-level min memory via annotation (MiB). Defaulting and clamping
	// will be applied in SandboxConfig (with RuntimeConfig) or later at send time.
	if v, ok := ocispec.Annotations[defs.ContainerMinMemMB]; ok && v != "" {
		if mb, err := strconv.ParseUint(v, 10, 32); err == nil {
			config.MemoryMinMB = uint32(mb)
		} else {
			log.Debugf("invalid %s: %s", defs.ContainerMinMemMB, v)
		}
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
	containerConfig, err := ContainerConfig(sbContainerID, bundle, *ocispec, cntr.PodSandbox, detach, rc.DefaultFirmwarePath)
	if err != nil {
		return cntr.SandboxConfig{}, err
	}
	if containerConfig.MemoryMinMB == 0 {
		if rc.MinContainerMemMB > 0 {
			containerConfig.MemoryMinMB = rc.MinContainerMemMB
		} else {
			containerConfig.MemoryMinMB = defs.DefaultMinMemMB
		}
	}
	// Clamp to limit if applicable
	if containerConfig.MemoryLimitMB > 0 && containerConfig.MemoryMinMB > containerConfig.MemoryLimitMB {
		containerConfig.MemoryMinMB = containerConfig.MemoryLimitMB
	}

	// TODO: allocated shared resources

	networkConfig := cntr.NetworkConfig{}
	ped := cntr.HostPedType
	if ped == pedestal.Xen {
		pedcfg := filepath.Join(bundleRootfs(bundle), defs.DefaultXenBin)
		log.Debugf("pedestal config for xen is the location of <%s>: %s",defs.DefaultXenBin, pedcfg)
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
		PedConfig: pedestal.PedestalConfig{
			PedType:     pedestal.GetHostPed(),
			PedConfig:   "",
			MiniVCPUNum: rc.MiniVCPUNum,
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
		HugePageSupport:    hugePage,
		EnableVCPUsPining:  false,
	}

	applySandboxAnnotations(*ocispec, &sandboxConfig)
	// Persist the resolved firmware path so later containers in the same sandbox can reuse it.
	if sandboxConfig.Annotations == nil {
		sandboxConfig.Annotations = make(map[string]string)
	}
	if containerConfig.ElfAbsPath != "" {
		sandboxConfig.Annotations[defs.FirmwarePath] = containerConfig.ElfAbsPath
	}
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

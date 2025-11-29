package oci

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	defs "micrun/definitions"
	log "micrun/logger"
	cntr "micrun/pkg/micantainer"
	"micrun/pkg/pedestal"
	"micrun/pkg/utils"

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
	// io.kubernetes.cri.container_type || io.kubernetes.cri-o.container_type
	CRIContainerTypeKeyList = []string{ctrAnnotations.ContainerType, podmanAnnotations.ContainerType}

	// CRISandboxNameKeyList lists all the CRI keys that could define
	// the sandbox ID from annotations in the config.json.
	// "io.kubernetes.cri.sandbox-id" || "io.kubernetes.cri-o.SandboxID"
	CRISandboxNameKeyList = []string{ctrAnnotations.SandboxID, podmanAnnotations.SandboxID}

	// CRIContainerTypeList lists all the maps from CRI ContainerTypes annotations
	// to a virtcontainers ContainerType.
	CRIContainerTypeList = []annotationContainerType{
		{ctrAnnotations.ContainerTypeSandbox, cntr.PodSandbox},
		{ctrAnnotations.ContainerTypeContainer, cntr.PodContainer},
		{podmanAnnotations.ContainerTypeSandbox, cntr.PodSandbox},
		{podmanAnnotations.ContainerTypeContainer, cntr.PodContainer},
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

// extPedConfig extracts and validates pedestal configuration from annotations.
// Returns pedestal type, config path, or error if validation fails.
// This includes checking pedestal type compatibility and resolving Xen image paths.
func extPedConfig(getAnnotation func(string) (string, bool), baseRootfs, id string) (pedestal.PedType, string, error) {
	pedtype := cntr.HostPedType
	// if pedType is not specified, use host ped type, skip matching
	if pedAnnoation, ok := getAnnotation(defs.Pedtype); ok {
		if pedAnnoation != pedtype.String() {
			log.Errorf("pedestal type mismatch, host pedestal is %s, while image container %s requires pedestal %s ", pedtype.String(), id, pedAnnoation)
			return pedtype, "", fmt.Errorf("hypervisor type mismatched: %s ~ %s", pedtype.String(), pedAnnoation)
		}
	}

	var pedconf string
	if cfg, ok := getAnnotation(defs.PedestalConf); ok {
		pedconf = cfg
		log.Debugf("pedestal config path from annotation: %s", pedconf)
	}

	if pedtype == pedestal.Xen {
		// Resolve Xen pedestal image path with annotation > default fallback
		pedconf = inBundlePath(baseRootfs, pedconf, defs.DefaultXenBin)
		log.Debugf("Resolved Xen pedestal config path: %s", pedconf)
	}

	return pedtype, pedconf, nil
}

// extOSInfo extracts OS name from annotations.
func extOSInfo(getAnnotation func(string) (string, bool)) string {
	var osName string
	if osAnno, ok := getAnnotation(defs.OSAnnotation); ok {
		osName = osAnno
		log.Debugf("found OS annotation: %s", osName)
	} else {
		log.Warnf("unable to know the RTOS type ")
	}
	return osName
}

// extFirmwareAnno extracts firmware path from annotations.
func extFirmwareAnno(getAnnotation func(string) (string, bool), baseRootfs string) string {
	var annotationFirmware string
	if fw, ok := getAnnotation(defs.FirmwarePathAnno); ok && fw != "" {
		annotationFirmware = inBundlePath(baseRootfs, fw, defs.DefaultFirmwareName)
		log.Debugf("using firmware from annotation: %s", annotationFirmware)
	}
	return annotationFirmware
}

// detectType determines container type based on annotations and spec.
func detectType(getAnnotation func(string) (string, bool), Type cntr.ContainerType, ocispec specs.Spec, annotationFirmware string) bool {
	hasMicrunAnn := false
	log.Pretty("spec annotations=%v", ocispec.Annotations)
	if ocispec.Annotations != nil {
		for key := range ocispec.Annotations {
			if strings.HasPrefix(key, defs.MicranAnnotationPrefix) {
				hasMicrunAnn = true
				break
			}
		}
	}

	hasCRIInfraAnnotation := false
	if ocispec.Annotations != nil {
		for _, key := range CRIContainerTypeKeyList {
			if v, ok := ocispec.Annotations[key]; ok {
				// just v == "sandbox"?
				if v == ctrAnnotations.ContainerTypeSandbox || v == podmanAnnotations.ContainerTypeSandbox {
					hasCRIInfraAnnotation = true
					break
				}
			}
		}
	}

	isCRISandbox := Type == cntr.PodSandbox
	log.Debugf("isCRISandbox?%v, hasCRIInfraAnnotation?%v, hasMicranAnnotation?%v, annotationFirmware=%s",
		isCRISandbox, hasCRIInfraAnnotation, hasMicrunAnn, annotationFirmware)

	return hasCRIInfraAnnotation
}

// resolveElfPath resolves firmware ELF file path with precedence: annotation > runtime default > discovery.
func resolveElfPath(baseRootfs string, isInfra bool, annotationFirmware string) (string, error) {
	var elfPath string
	if !isInfra && annotationFirmware != "" {
		elfPath = getBundleImageFile(baseRootfs, annotationFirmware)
		if elfPath == "" {
			return "", fmt.Errorf("firmware file not found: %s", annotationFirmware)
		}
	}

	if elfPath == "" {
		defaultElfPath := getBundleImageFile(baseRootfs, defs.DefaultFirmwareName)
		if defaultElfPath != "" {
			elfPath = defaultElfPath
			log.Debugf("using default elf path: %s", elfPath)
		} else {
			elfFiles, _ := filepath.Glob(filepath.Join(baseRootfs, "*.elf"))
			if len(elfFiles) > 0 {
				elfPath = elfFiles[0]
				log.Debugf("found elf file: %s", elfPath)
			} else {
				return "", fmt.Errorf("no elf file found in container rootfs and no firmware path provided via annotation or runtime configuration")
			}
		}
	}
	return elfPath, nil
}

// prepCache prepares container cache directory and copies firmware/pedestal files to safe location.
func prepCache(id, baseRootfs, pedconf, elfPath string) (string, string, error) {
	// Create a dedicated directory for the container to cache firmware, image, etc.
	// This avoids race conditions with the bundle being unmounted by containerd.
	containerCacheDir := filepath.Join(defs.DefaultMicaContainersRoot, id)
	if err := os.MkdirAll(containerCacheDir, defs.DirMode); err != nil {
		return "", "", fmt.Errorf("failed to create container cache directory %s: %w", containerCacheDir, err)
	}

	// copyToCache copies a file to the container's cache directory if it's a valid file.
	// It returns the new path or the original path if copying is not possible/needed.
	copyToCache := func(sourcePath string) (string, error) {
		if sourcePath == "" {
			return "", nil
		}
		// We only copy if the source is a regular file. If it's something else (e.g. a pipe or doesn't exist),
		// we pass it along as-is and let the consumer (micad) deal with it. This is to avoid
		// breaking cases where the path might not be a simple file.
		stat, err := os.Stat(sourcePath)
		if err != nil {
			return sourcePath, nil
		}
		if !stat.Mode().IsRegular() {
			return sourcePath, nil
		}

		destPath := filepath.Join(containerCacheDir, filepath.Base(sourcePath))

		sourceFile, err := os.Open(sourcePath)
		if err != nil {
			return "", fmt.Errorf("failed to open source file %s: %w", sourcePath, err)
		}
		defer sourceFile.Close()

		destFile, err := os.Create(destPath)
		if err != nil {
			return "", fmt.Errorf("failed to create destination file %s: %w", destPath, err)
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, sourceFile); err != nil {
			return "", fmt.Errorf("failed to copy from %s to %s: %w", sourcePath, destPath, err)
		}
		log.Debugf("copied %s to safe location %s", sourcePath, destPath)
		return destPath, nil
	}

	var err error
	if pedconf, err = copyToCache(pedconf); err != nil {
		return "", "", err
	}
	if elfPath, err = copyToCache(elfPath); err != nil {
		return "", "", err
	}
	return pedconf, elfPath, nil
}

// container bundle rootfs is already mounted
// hence we can check bundle contents for container configuration
func ContainerConfig(id, bundle string, ocispec specs.Spec, Type cntr.ContainerType, detach bool, defaultFirmwarePath string, runtimeConfig *RuntimeConfig) (*cntr.ContainerConfig, error) {
	baseRootfs := bundleRootfs(bundle)

	getAnnotation := func(key string) (string, bool) {
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

	pedtype, pedconf, err := extPedConfig(getAnnotation, baseRootfs, id)
	if err != nil {
		return nil, err
	}

	osName := extOSInfo(getAnnotation)

	annotationFirmware := extFirmwareAnno(getAnnotation, baseRootfs)

	isInfra := detectType(getAnnotation, Type, ocispec, annotationFirmware)

	elfPath, err := resolveElfPath(baseRootfs, isInfra, annotationFirmware)
	if err != nil {
		return nil, err
	}

	pedconf, elfPath, err = prepCache(id, baseRootfs, pedconf, elfPath)
	if err != nil {
		return nil, err
	}

	// init
	config := &cntr.ContainerConfig{
		// Container ID
		ID: id,
		// OCI and bundle info
		ImageAbsPath: elfPath,
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

	// Legacy PTY mode via annotation (default: true)
	config.LegacyPty = true // default to legacy PTY mode
	if v, ok := ocispec.Annotations[defs.LegacyPty]; ok && v != "" {
		if legacyPty, err := strconv.ParseBool(v); err == nil {
			config.LegacyPty = legacyPty
			log.Debugf("found legacy PTY annotation: %s = %t", defs.LegacyPty, legacyPty)
		} else {
			log.Debugf("invalid %s: %s, using default (true)", defs.LegacyPty, v)
		}
	}

	// Validate resource limits against system constraints
	applyContainerRuntimeDefaults(config, ocispec.Annotations, runtimeConfig)
	if err := cntr.ValidateResourceLimits(config); err != nil {
		log.Warnf("resource validation warning: %v", err)
		// Don't fail the container creation for resource validation warnings
		// but log them for visibility
	}

	// OS is already set from annotation or default above
	log.Debugf("container OS: %s", config.OS)

	log.Debugf("container resource limits - CPU: %s, Memory: %s",
		formatCPULimit(config), formatMemoryLimit(config))
	return config, nil
}

func SandboxConfig(ocispec *specs.Spec, rc RuntimeConfig, bundle, sbContainerID string, detach bool) (cntr.SandboxConfig, error) {
	// generate sandbox container config
	containerConfig, err := ContainerConfig(sbContainerID, bundle, *ocispec, cntr.PodSandbox, detach, rc.DefaultFirmwarePath, &rc)
	if err != nil {
		return cntr.SandboxConfig{}, err
	}
	// TODO: allocated shared resources

	networkConfig := cntr.NetworkConfig{}
	ped := cntr.HostPedType
	if ped == pedestal.Xen {
		pedcfg := filepath.Join(bundleRootfs(bundle), defs.DefaultXenBin)
		log.Debugf("pedestal config for xen is the location of <%s>: %s", defs.DefaultXenBin, pedcfg)
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
			// Use host pedestal type and resolved pedestal config path from container config.
			PedType:     pedestal.GetHostPed(),
			PedConfig:   containerConfig.PedestalConf,
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
		InfraOnly:          containerConfig.IsInfra,
	}

	applySandboxAnnotations(*ocispec, &sandboxConfig)
	// Persist the resolved firmware path so later containers in the same sandbox can reuse it.
	if sandboxConfig.Annotations == nil {
		sandboxConfig.Annotations = make(map[string]string)
	}
	if containerConfig.ImageAbsPath != "" {
		sandboxConfig.Annotations[defs.FirmwarePathAnno] = containerConfig.ImageAbsPath
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

func applyContainerRuntimeDefaults(config *cntr.ContainerConfig, annotations map[string]string, runtimeConfig *RuntimeConfig) {
	if config == nil {
		return
	}

	runtimeCfg := runtimeConfig
	if runtimeCfg == nil {
		runtimeCfg = NewRuntimeConfig()
	}

	if config.MemoryMinMB == 0 {
		if runtimeCfg.MinContainerMemMB > 0 {
			config.MemoryMinMB = runtimeCfg.MinContainerMemMB
		} else {
			config.MemoryMinMB = defs.DefaultMinMemMB
		}
	}

	if config.MemoryLimitMB > 0 && config.MemoryMinMB > config.MemoryLimitMB {
		config.MemoryMinMB = config.MemoryLimitMB
	}

	config.MaxVcpuNum = resolveMaxVcpu(annotations, runtimeCfg)
	config.MemoryThresholdMB = calculatePedMaxMemory(config, runtimeCfg)
}

func resolveMaxVcpu(annotations map[string]string, runtimeCfg *RuntimeConfig) uint32 {
	if annotations != nil {
		if value, ok := annotations[defs.ContainerMaxVcpuNum]; ok && value != "" {
			if parsed, err := strconv.ParseUint(value, 10, 32); err == nil && parsed > 0 {
				return uint32(parsed)
			} else if err != nil {
				log.Debugf("invalid %s %q: %v", defs.ContainerMaxVcpuNum, value, err)
			}
		}
	}

	if runtimeCfg != nil && runtimeCfg.MaxContainerVCPUs > 0 {
		return runtimeCfg.MaxContainerVCPUs
	}

	return defaultMaxContainerVCPUs
}

func calculatePedMaxMemory(config *cntr.ContainerConfig, runtimeCfg *RuntimeConfig) uint32 {
	maxMem := config.MemoryLimitMB
	if maxMem == 0 {
		maxMem = config.MemoryMinMB
	}
	if maxMem == 0 && runtimeCfg != nil && runtimeCfg.MinContainerMemMB > 0 {
		maxMem = runtimeCfg.MinContainerMemMB
	}
	if maxMem == 0 {
		maxMem = defs.DefaultMinMemMB
	}
	if runtimeCfg != nil && runtimeCfg.MaxContainerMemMB > 0 && maxMem > runtimeCfg.MaxContainerMemMB {
		maxMem = runtimeCfg.MaxContainerMemMB
	}

	if maxMem > math.MaxUint32/2 {
		return math.MaxUint32
	}

	doubled := maxMem * 2
	if doubled == 0 {
		doubled = defs.DefaultMinMemMB * 2
	}
	return doubled
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
		if !strings.HasPrefix(key, defs.MicranAnnotationPrefix) || value == "" {
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

// getBundleImageFile now does path conversion and simple file existence check
func getBundleImageFile(dir, p string) string {
	rp := bundleFilePath(dir, p)
	if rp == "" {
		return ""
	}
	if abs, err := utils.ResolvePath(rp); err == nil {
		log.Debugf("resolved path (to be validated later): %s -> %s", p, abs)
		if utils.FileExist(abs) {
			return abs
		}
	}
	return ""
}

// inBundlePath selects a path from annotation with fallback to default value.
// This function implements simple precedence: annotationValue > defaultValue.
// It returns the selected path as-is without converting it to a host path.
// Path conversion to host filesystem is done later by getBundleImageFile.
//
// The baseRootfs parameter is unused but preserved for API consistency and future extensibility.
func inBundlePath(baseRootfs, annotationValue, defaultValue string) string {
	value := annotationValue
	if value == "" {
		value = defaultValue
	}
	return value
}

// resolve a container bundle file's path (absolute inside container or relative) to a host path under baseRootfs.
// p = "/absolute-path-to-rootfs" => "$baseRootfs/relative-path-to-rootfs"
// p = "relateive-path-to-rootfs" => "$baseRootfs/relative-path-to-rootfs"
// p = "" => ""
func bundleFilePath(dir, p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Join(dir, strings.TrimPrefix(trimmed, string(filepath.Separator)))
	}
	return filepath.Join(dir, trimmed)
}

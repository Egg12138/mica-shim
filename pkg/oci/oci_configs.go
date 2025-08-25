package oci

import (
	"fmt"
	"path/filepath"
	"strings"

	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/fileutils"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/pedestal"

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

func ContainerConfig(bundle string, ocispec specs.Spec, cType cntr.ContainerType, detach bool) (*cntr.ContainerConfig, error) {
	configPath := filepath.Join(bundle, "rootfs", defs.DefaultClientConf)
	micaConf, err := fileutils.ParseConfigINI(configPath)
	if err != nil {
		return nil, err
	}

	config := &cntr.ContainerConfig{
		// OCI and bundle info
		ElfPath:      micaConf[defs.ElfPath],
		PedestalType: pedestal.Unsupported,
		PedestalConf: "",
		OS:           "",
		NCpu:         1,
		CpuLimit:     0,
		CpusetCpus:   "",
		CpuShares:    0,
		CpuQuota:     0,
		CpuPeriod:    0,

		// Memory defaults
		MemoryLimit:       0,
		MemoryReservation: 0,
		MemorySwap:        0,
		MemoryKernel:      0,
		MemorySwappiness:  nil,
		OomKillDisable:    false,
	}

	if err := config.ParseOCICPUResources(&ocispec); err != nil {
		return nil, err
	}

	if err := config.ParseOCIMemoryResources(&ocispec); err != nil {
		return nil, err
	}

	// Validate resource limits against system constraints
	if err := cntr.ValidateResourceLimits(config); err != nil {
		log.Warnf("Resource validation warning: %v", err)
		// Don't fail the container creation for resource validation warnings
		// but log them for visibility
	}

	// Set default OS if not specified
	if config.OS == "" {
		log.Warn("os is not set, default to zephyr")
		config.OS = "zephyr"
	}

	log.Infof("Container resource limits - CPU: %s, Memory: %s",
		formatCPULimit(config), formatMemoryLimit(config))
	return config, nil
}

func SandboxConfig(ocispec specs.Spec, runtime RuntimeConfig, bundle, cid string, detach bool) (cntr.SandboxConfig, error) {
	return cntr.SandboxConfig{}, nil
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

	if config.MemoryLimit > 0 {
		parts = append(parts, fmt.Sprintf("limit=%s", formatBytes(config.MemoryLimit)))
	}

	if config.MemoryReservation > 0 {
		parts = append(parts, fmt.Sprintf("reservation=%s", formatBytes(config.MemoryReservation)))
	}

	if config.MemorySwap > 0 {
		parts = append(parts, fmt.Sprintf("swap=%s", formatBytes(config.MemorySwap)))
	}

	if config.MemoryKernel > 0 {
		parts = append(parts, fmt.Sprintf("kernel=%s", formatBytes(config.MemoryKernel)))
	}

	if config.MemorySwappiness != nil {
		parts = append(parts, fmt.Sprintf("swappiness=%d", *config.MemorySwappiness))
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

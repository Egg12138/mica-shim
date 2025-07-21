package cntr

import (
	"errors"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	ctrAnnotations "github.com/containerd/containerd/pkg/cri/annotations"
	podmanAnnotations "github.com/containers/podman/v4/pkg/annotations"
	dockershimAnnotations "github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/annotations/dockershim"
)

type annotationContainerType struct {
	annotation    string
	containerType ContainerType
}

// CRI types list reference: kata-containers
var (
	// CRIContainerTypeKeyList lists all the CRI keys that could define
	// the container type from annotations in the config.json.
	CRIContainerTypeKeyList = []string{ctrAnnotations.ContainerType, podmanAnnotations.ContainerType, dockershimAnnotations.ContainerTypeLabelKey}

	// CRISandboxNameKeyList lists all the CRI keys that could define
	// the sandbox ID (sandbox ID) from annotations in the config.json.
	CRISandboxNameKeyList = []string{ctrAnnotations.SandboxID, podmanAnnotations.SandboxID, dockershimAnnotations.SandboxIDLabelKey}

	// CRIContainerTypeList lists all the maps from CRI ContainerTypes annotations
	// to a virtcontainers ContainerType.
	CRIContainerTypeList = []annotationContainerType{
		{podmanAnnotations.ContainerTypeSandbox, PodSandbox},
		{podmanAnnotations.ContainerTypeContainer, PodContainer},
		{ctrAnnotations.ContainerTypeSandbox, PodSandbox},
		{ctrAnnotations.ContainerTypeContainer, PodContainer},
		{dockershimAnnotations.ContainerTypeLabelSandbox, PodSandbox},
		{dockershimAnnotations.ContainerTypeLabelContainer, PodContainer},
	}
)

func inList(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

// TODO: get the MAX Managable cpu nums of different pedestals
func pedestalMaxCPU() int {
	// FIXME: a placeholder
	return physicalMaxCPU()
}

func physicalMaxCPURobust() int {

	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		log.Warnf("failed to read /proc/cpuinfo, falling back to runtime.NumCPU(): %v", err)
		return runtime.NumCPU()
	}

	// Parse physical CPU IDs to count unique physical processors
	physicalNums := 0
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "physical id") {
			physicalNums++
		}
	}

	if physicalNums > 0 {
		return physicalNums
	}

	return runtime.NumCPU()
}

// physical Max CPU in the perspective of container runtime!
func physicalMaxCPU() int {
	// log.Debugf("no physical CPU IDs found in /proc/cpuinfo, using runtime.NumCPU()")
	log.Debugf("Currently use runtime.NumCPU()")
	return runtime.NumCPU()
}

// get the most CPU nums the machine provided and current pedestal manager supports
// TODO: complete the logic
// From mcs_km.c
// mpidr = get_cpu_mpidr(info.cpu);  // Maps to physical CPU
// if (cpu >= NR_CPUS)               // Validates against physical CPU count
//
//	return INVALID_HWID;
func availableMaxCPU() int {
	m := min(physicalMaxCPU(), pedestalMaxCPU())
	if m > 1 {
		m -= 1
	}
	log.Debugf("availableMaxCPU: %d", m)
	return m
}

func getNcpu(v string) int {
	ncpu, err := strconv.Atoi(v)
	if err != nil {
		log.Warnf("failed to parse ncpu(int) label from %s, set to %d: %v", v, defs.DefaultNcpu, err)
	} else if ncpu > HostMaxCPU {
		log.Warnf("ncpu(int) label from %s is greater than the available max CPU, set to %d", v, availableMaxCPU())
	} else if ncpu < 1 {
		log.Warnf("ncpu(int) label from %s is less than 1, set to %d", v, defs.DefaultNcpu)
	} else {
		return ncpu
	}

	return defs.DefaultNcpu
}

// TODO: multi cpu
// system-wide sharememory-based scheduler
func allocCPU(ncpu int) (int, error) {
	// Validate ncpu parameter
	if ncpu < 1 {
		return 0, fmt.Errorf("ncpu must be at least 1, got %d", ncpu)
	}

	maxCPU := HostMaxCPU
	if ncpu > maxCPU {
		return 0, fmt.Errorf("requested ncpu %d exceeds available max CPU %d", ncpu, maxCPU)
	}

	// Simple round-robin allocation based on current time
	// In a real implementation, this would track allocated CPUs
	// For now, just return a CPU ID within the available range
	allocatedCPU := int(time.Now().UnixNano()) % maxCPU

	log.Debugf("Allocated CPU %d for ncpu=%d (max available: %d)", allocatedCPU, ncpu, maxCPU)
	return allocatedCPU, nil
}

// check OS value matches
func validOS(os string) bool {
	ret := inList(defs.PreservedOS[:], os)
	log.Debugf("validating OS: %s, result: %v", os, ret)
	return ret
}

func validFirmware(root, firmware string) bool {
	// <bundle>/rootfs/<firmware>
	log.Debugf("validating firmware: %s", firmware)
	resolved, err := resolvePath(filepath.Join(root, firmware))
	if err != nil {
		return false
	}
	ret := fileExists(resolved)
	log.Debugf("current firmware path is: %s. valid = %v", resolved, ret)
	return ret
}

func validCompatibility(info *MicaContainerInfo) bool {
	// TODO: needed to ? how to check compatibility?
	return true
}

// Recursively walk the directory and return a string of the directory tree
// Used for debug
func walkDir(dir string) string {
	// Build a human-readable directory tree for debug purposes.
	// On any failure it returns a string prefixed with "Walk error:" describing the problem.
	var builder strings.Builder

	root, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Sprintf("Walk error: %v", err)
	}

	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(os.PathSeparator))
		}
		indent := strings.Repeat("  ", depth)

		if info.IsDir() {
			builder.WriteString(fmt.Sprintf("%s%s/\n", indent, info.Name()))
		} else {
			builder.WriteString(fmt.Sprintf("%s%s\n", indent, info.Name()))
		}
		return nil
	})

	if walkErr != nil {
		return fmt.Sprintf("Walk error: %v", walkErr)
	}

	return builder.String()
}

func detectXen() int {
	if _, err := os.Stat("/proc/xen"); err != nil {
		return 0
	}
	return 1
}

func detectJailhouse() int {
	if _, err := os.Stat("/sys/devices/jailhouse"); err != nil {
		return 0
	}
	if _, err := os.Stat("/usr/share/jailhouse"); err != nil {
		return 0
	}
	if _, err := os.Stat("/etc/modules-load.d/jailhouse.conf"); err != nil {
		return 0
	}

	kernelRelease, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err == nil {
		release := strings.TrimSpace(string(kernelRelease))
		jailhouseKoPath := fmt.Sprintf("/lib/modules/%s/extra/driver/jailhouse.ko", release)
		if _, err := os.Stat(jailhouseKoPath); err != nil {
			return 0
		}
	}

	_, err = filepath.Glob("/usr/libexec/jailhouse/jailhouse-*")
	if err != nil {
		return 0
	}

	return 1
}

func detectBaremetal() int {
	// check loaded Kerkenl modules contains "mcs":
	kernelRelease, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return 1
	if err == nil {
		release := strings.TrimSpace(string(kernelRelease))
		mcsKoPath := fmt.Sprintf("/lib/modules/%s/extra/mcs.ko", release)
		if _, err := os.Stat(mcsKoPath); err != nil {
			return 0
		}
	}

	if _, err := os.Stat("/etc/modules-load.d/mcs.conf"); err != nil {
		return 0
	}

	if _, err := os.Stat("/usr/share/mcs"); err != nil {
		return 0
	}

	return 1
}

// TODO: mark this information a host-level config, the "guessing" only needs once
// 'Guess' what pedestal the host is on
func hostPed() PedType {
	// weights := []int{detectXen(), detectJailhouse(), detectBaremetal()}
	weights := []int{detectBaremetal(), detectJailhouse(), detectXen()}
	index := 1*weights[Baremetal] + 2*weights[Jailhouse] + 3*weights[Xen] - 1
	if index < 0 || index > 2 {
		return Unknown
	}
	return PedType(index)
}

// Currently, one host only support one pedestal type.
func hostPedMatched(ped *Pedestal, os string) bool {
	ret := HostPedestalType == ped.PedestalType
	log.Debugf("hostPedMatched: %v, %s, result: %v", ped, os, ret)
	log.Debugf("hostPedestalType: %v, ped.PedestalType: %v", HostPedestalType, ped.PedestalType)
	return ret
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

func setReadonly(path string) error {
	// assume path is a valid direntry
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0444)
		if info.IsDir() {
			mode = os.FileMode(0555)
		}
		return os.Chmod(path, mode)
	})
}

// bundle is <CONTINAER_STATE_ROOT>/<container_id>
func setupBundle(bundle string) error {

	// config := filepath.Join(bundle, "config.json")
	rootfs := filepath.Join(bundle, "rootfs")

	// TODO: recursively chmod 0555
	if err := setReadonly(rootfs); err != nil {
		return fmt.Errorf("failed to chmod rootfs: %w", err)
	}
	os.Chdir(bundle)
	return nil
}

func validBundle(containerID, bundlePath string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("container ID is empty")
	}

	if bundlePath == "" {
		return "", fmt.Errorf("missing bundle path")
	}

	// bundle path MUST be valid.
	fileInfo, err := os.Stat(bundlePath)
	if err != nil {
		return "", fmt.Errorf("invalid bundle path '%s': %s", bundlePath, err)
	}
	if !fileInfo.IsDir() {
		return "", fmt.Errorf("invalid bundle path '%s', it should be a directory", bundlePath)
	}

	rootfs := filepath.Join(bundlePath, "rootfs")
	fileInfo, err = os.Stat(rootfs)
	if err != nil {
		return "", fmt.Errorf("%s requires rootfs in bundle, invalid rootfs path '%s': %s", defs.RuntimeName, rootfs, err)
	}
	if !fileInfo.IsDir() {
		return "", fmt.Errorf("%s requires rootfs in bundle, invalid rootfs path '%s', it should be a directory", defs.RuntimeName, rootfs)
	}

	if err := setupBundle(bundlePath); err != nil {
		return "", fmt.Errorf("failed to setup bundle: %w", err)
	}

	// get a valid expanded path
	resolved, err := resolvePath(bundlePath)
	if err != nil {
		return "", err
	}

	return resolved, nil
}

// resolvePath returns the fully resolved and expanded value of the
// specified path.
func resolvePath(path string) (string, error) {
	if path == "" {
		log.FDebugf("path must be specified")
		return "", fmt.Errorf("path must be specified")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file %v does not exist", absolute)
		}

		return "", err
	}

	return resolved, nil
}

func presetSandbox() {}

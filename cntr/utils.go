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

// physical Max CPU in the perspective of container runtime!
func physicalMaxCPU() int {
	
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
	
	log.Debugf("no physical CPU IDs found in /proc/cpuinfo, using runtime.NumCPU()")
	return runtime.NumCPU()
}

// get the most CPU nums the machine provided and current pedestal manager supports
// TODO: complete the logic
// From mcs_km.c
// mpidr = get_cpu_mpidr(info.cpu);  // Maps to physical CPU
// if (cpu >= NR_CPUS)               // Validates against physical CPU count
//     return INVALID_HWID;
func availableMaxCPU() int {
	return min(physicalMaxCPU(), pedestalMaxCPU())
}

func getNcpu(v string) int {
	ncpu, err := strconv.Atoi(v)
	if err != nil {
		log.Warnf("failed to parse ncpu(int) label from %s, set to %s: %v", v, defs.DefaultNcpu, err)
	} else if ncpu > availableMaxCPU() {
		log.Warnf("ncpu(int) label from %s is greater than the available max CPU, set to %d", v, availableMaxCPU())
	} else if ncpu < 1 {
		log.Warnf("ncpu(int) label from %s is less than 1, set to %s", v, defs.DefaultNcpu)
	} else {
		return ncpu
	}

	return defs.DefaultNcpu
}

// TODO: multi cpu
// sharememory-based scheduler
func allocCPU() (int, error) {
	// TODO: alloc cpu
	return 1, nil
}

// check OS value matches
func validOS(os string) bool {
	return inList(defs.PreservedOS[:], os)
}

func validFirmware(root, firmware string) bool {
	// <bundle>/rootfs/<firmware>
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

func hostPed() *Pedestal {
	// TODO: get host pedestal
	return &Pedestal{
		PedestalType: Baremetal,
		PedestalConf: "",
	}
}

// Currently, one host only support one pedestal type.
func hostPedMatched(ped *Pedestal, os string) bool {
	currentHost := hostPed()
	if currentHost.PedestalType != ped.PedestalType {
		return false
	}
	return true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
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

	rootfsExists := fileExists(rootfs)
	log.Debugf("rootfs <%s> Exists: %v", rootfs, rootfsExists)
	// TODO: mount rootfs
	if !rootfsExists {
		if err := os.MkdirAll(rootfs, 0755); err != nil {
			return fmt.Errorf("failed to create rootfs: %w", err)
		}
	}

	// TODO: recursively chmod 0555
	if err := setReadonly(rootfs); err != nil {
		return fmt.Errorf("failed to chmod rootfs: %w", err)
	}
	os.Chdir(bundle)
	return nil
}

func validBundle(containerID, bundlePath string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("missing container ID")
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
		log.LocateDebugf("path must be specified")
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
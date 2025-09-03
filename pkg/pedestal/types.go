package pedestal

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/fileutils"
	"os/exec"
	"strings"
	"time"
)

type PedType int
type PedConfig string

const (
	Xen PedType = iota
	FusionDock
	ACRN
	Baremetal
	Unsupported
)

// String returns the string representation of PedType
func (p PedType) String() string {
	switch p {
	case Xen:
		return "xen"
	default:
		return "unknown"
	}
}

func ParsePedType(s string) PedType {
	switch strings.ToLower(s) {
	case "xen", "":
		return Xen
	default:
		return Unsupported // default to baremetal
	}
}

func HostPed() PedType {
	if defs.IsMock {
		return Xen
	}
	if detectXen() {
		return Xen
	}

	if detectACRN() {
		return ACRN
	}
	return Unsupported
}

func detectXen() bool {
	// xl binary exist
	if !fileutils.FileExist("/proc/xen/xenbus") {
		log.Debug("missing xen bus")
		return false
	}

	if err := checkXLCommand(); err != nil {
		log.Debug("xl command not found or not working correctly")
		return false
	}

	// TODO: check new xen ko
	if err := checkXenKos(); err != nil {
		log.Debug("xen kernel modules requirements not met")
		return false
	}

	return true
}

func checkXenKos() error {
	// xen_netback, xen_blkback, xen_gntalloc, xen_gntdev
	// TODO: migrate xen-essentials ko to mica-xen related ko
	essentials := []string{"xen_gntalloc", "xen_gntdev"}
	for i, ko := range essentials {
		loaded, err := fileutils.KoLoaded(ko)
		if err != nil {
			return err
		}
		if !loaded {
			return fmt.Errorf("xl: %s is not loaded", essentials[i])
		}
	}
	return nil
}

func checkXLCommand() error {
	path, err := exec.LookPath("xl")
	if err != nil {
		return fmt.Errorf("xl not found in PATH: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	// 3. 执行命令并捕获输出
	cmd := exec.CommandContext(ctx, path, "vcpu-list")
	output, err := cmd.CombinedOutput() // 合并stdout/stderr

	if err != nil {
		return fmt.Errorf("command failed: %v\nOutput: %s", err, output)
	}

	if len(output) == 0 {
		return fmt.Errorf("command produced no output")
	}

	return nil
}

func detectACRN() bool {
	return false
}

// TODO: use interface to handle so many different pedestal
type PedTraits interface {
	ToString() string
	GeneratePedConf() (PedConfig, error)
}
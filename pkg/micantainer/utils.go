package micantainer

import (
	"context"
	"fmt"
	"mica-shim/pkg/libmica"

	"github.com/containerd/errdefs"
)

func createContainerInSandbox(ctx context.Context, sandbox SandboxTraits, config *ContainerConfig) (*RTOSTask, error) {
	return nil, errdefs.ErrNotImplemented
}

func startClient(ctx context.Context, sandbox SandboxTraits, c *Container) error {
	// TODO" 
	conf, err := createMicaConf(c)
	if err != nil {
		return err
	}

	if err = libmica.Create(conf); err != nil {
		return err
	}
	return nil
}

// 1. search bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TODO: Only copy values, the evaluation procedure is in the caller function
func createMicaConf(container *Container) (libmica.MicaClientConf, error) {
	config := container.config
	firmware := container.GetFirmwarePath()
	pedestal := HostPedType
	name := container.ID()
	cpu, err := container.GetClientCPU()
	conf := libmica.MicaClientConf{}
	if err != nil {
		return conf, fmt.Errorf("failed to get client cpu: %w", err)
	}
	mem := uint64(config.MemoryLimit)
	conf.InitWithOpts(libmica.MicaClientConfCreateOptions{
		CPU:      []int{cpu},
		// TODO: dummy settings
		CPUCapacity: int(config.CpuQuota),
		CPUWeight: int(config.CpuShares),
		Debug:    false,
		Memory:   int(mem),
		Name:     name,
		Network:  "",
		Path:     firmware,
		Ped:      pedestal.String(),
	})
	return conf, nil
}


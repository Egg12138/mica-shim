package shim

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/micantainer"
	"mica-shim/pkg/oci"
	"os"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
)

func createContainer(ctx context.Context, r *taskAPI.CreateTaskRequest) (*cntr.Container, error) {
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
	containerType, err := oci.ContainerType(*ociSpec)
	if err != nil {
		return nil, err
	}

}

// load from bundle first, then from micran state dir
func loadContainerState(id string) (*cntr.State, error) {
	cwd, err := os.Getwd()
	log.Debugf("cwd: %s", cwd)
	var st *cntr.State
	if err == nil {
		if st, err = cntr.LoadStateFromDir(cwd); err == nil {
			return st, nil
		}
	}

	stateDir := filepath.Join(defs.MicranStateDir, id)
	log.Debugf("load container state from %s: %v. try to load from micran state dir: %s", cwd, err, stateDir)

	st, err = cntr.LoadStateFromDir(stateDir)

	if err == nil && st != nil {
		if !utils.IdMatched(id, st.ID) {
			return nil, fmt.Errorf("container id mismatch: %s != %s", id, st.ID)
		}
		return st, nil
	}

	return nil, err
}

func setupMicranStateDir() error {
	if err := os.MkdirAll(defs.MicranStateDir, 0755); err != nil {
		return fmt.Errorf("failed to create micran state directory: %w", err)
	}
	return nil
}

func saveContainerState(c *cntr.Container) error {
	if err := os.MkdirAll(filepath.Join(defs.MicranStateDir, c.ID), 0o755); err != nil {
		log.Debugf("failed to create <%s> state directory: %v", c.ID)
		return err
	}
	return c.SaveState()
}

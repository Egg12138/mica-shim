// Package shim provides the implementation of the containerd shim v2 interface for micran.
package shim

import (
	"context"
	"errors"
	er "mica-shim/errors"
	log "mica-shim/logger"
	"path/filepath"

	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/mount"
)

// deleteContainer handles the deletion of a container, including stopping it if necessary and unmounting its rootfs.
func deleteContainer(ctx context.Context, s *shimService, c *container) error {
	if c == nil {
		return nil
	}

	// Forcibly delete pod containers.
	if !c.cType.CanBeSandbox() {
		if c.status != task.Status_STOPPED {
			if _, err := s.sandbox.StopContainer(ctx, c.id, false); err != nil && errors.Is(err, er.ErrContainerNotFound) {
				log.Infof("Container %s not found in real sandbox, already deleted.", c.id)
			} else {
				return err
			}
		}
		if c, err := s.sandbox.DeleteContainer(ctx, c.id); err != nil && errors.Is(err, er.ErrContainerNotFound) {
			log.Infof("Container %s not found in real sandbox, already deleted.", c.ID())
			return err
		}
	}

	if c.mounted {
		innerRootfs := filepath.Join(c.bundle, "rootfs")
		if err := mount.UnmountAll(innerRootfs, 0); err != nil {
			return err
		}
	}

	delete(s.containers, c.id)

	return nil
}
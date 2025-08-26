package shim

import (
	"context"
	"errors"
	log "mica-shim/logger"
	er "mica-shim/pkg/errors"
	"path/filepath"

	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/mount"
)

func deleteContainer(ctx context.Context, s *shimService, c *container) error {
	if c == nil {
		return nil
	}

	if !c.cType.CanBeSandbox() {
		if c.status != task.Status_STOPPED {
			if _, err := s.sandbox.StopContainer(ctx, c.id, false); err != nil && errors.Is(err, er.ErrContainerNotFound) {
				log.Infof("container %s not found in real sandbox, already deleted", c.id)
			} else {
				return err
			}
		}
		if c, err := s.sandbox.DeleteContainer(ctx, c.id); err != nil && errors.Is(err, er.ErrContainerNotFound) {
			log.Infof("container %s not found in real sandbox, already deleted", c.ID())
			return err	
		}

	}

	if c.mounted {
		innerRootfs := filepath.Join(c.bundle, "rootfs")
		if err := mount.UnmountAll(innerRootfs, 0); err != nil {
			return err
		}
	}
	
	// s.containers[c.id] will not be removed until reference count is zero
	delete(s.containers, c.id)

	return nil
}

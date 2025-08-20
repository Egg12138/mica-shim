package shim

import (
	"context"
	"fmt"
	log "mica-shim/logger"

	"github.com/containerd/containerd/api/types/task"
)

func startContainer(ctx context.Context, s *shimService, c *container) error {

	defer func() {
		if retErr != nil {
			c.exitCh <- 255
		}
	}()

	if c.cType == "" {
		err := fmt.Errorf("the contaienr %s type is empty", c.id)
		return err
	}

	if s.sandbox == nil {
		err := fmt.Errorf("the sandbox hasn't been created for this container %s", c.id)
		return err
	}

	if c.cType.CanBeSandbox() {
		err := s.sandbox.Start(ctx)
		if err != nil {
			return err
		}
	} else {
		_, err := s.sandbox.StartContainer(ctx, c.id)
		if err != nil {
			return err
		}
	}


	c.status = task.Status_RUNNING
	stdin, stdou, stderr, err := s.sandbox.IOStream(c.id, c.id)
	if err != nil {
		return err
	}
	log.Debugf("=> io stream: %v %v %v", stdin, stdou, stderr)

	c.stdinPipe = stdin

	go wait(ctx, s, c, "")


	return nil
}
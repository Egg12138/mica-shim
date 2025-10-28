package shim

import (
	"context"
	"fmt"
	log "mica-shim/logger"

	"github.com/containerd/containerd/api/types/task"
)

func startContainer(ctx context.Context, s *shimService, c *container) (retErr error) {

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
		log.Debugf("container %s can be sandbox, trying to start it now", c.id)
		err := s.sandbox.Start(ctx)
		if err != nil {
			log.Errorf("failed to start sandbox for container %s", c.id)
			return err
		}

		go watchSandbox(ctx, s)
	} else {
		_, err := s.sandbox.StartContainer(ctx, c.id)
		if err != nil {
			return err
		}
	}

	oldst := c.status
	c.status = task.Status_RUNNING
	log.Debugf("container status from %s => %s ", oldst, c.status)
	stdin, stdout, stderr, err := s.sandbox.IOStream(c.id, c.id)
	if err != nil {
		return err
	}
	log.Debugf("=> io stream: %v %v %v", stdin, stdout, stderr)

	c.stdinPipe = stdin

	if c.stdin != "" || c.stdout != "" || c.stderr != "" {
		tty, err := newTtyIO(ctx, c.id, c.stdin, c.stdout, c.stderr, c.terminal)
		if err != nil {
			return err
		}
		c.ttyio = tty

		go ioCopy(c.exitIOch, c.stdinCloser, tty, stdin, stdout)
	} else {
		// close the io exit channel, since there is no io for this container,
		// otherwise the following wait goroutine will hang on this channel.
		close(c.exitIOch)
		// close the stdin closer channel to notify that it's safe to close process's
		// io.
		close(c.stdinCloser)
	}

	go waitContainerExit(ctx, s, c)

	return nil
}

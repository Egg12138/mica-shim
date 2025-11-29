package shim

import (
	"context"
	"fmt"
	log "micrun/logger"

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
		// Close stdin closer so CloseIO can unblock even when the container never
		// had an input fifo.
		close(c.stdinCloser)
		// Infra (pause) containers must stay alive to keep the sandbox ready.
		// Skip closing exitIOch so waitContainerExit only runs when we receive an
		// explicit teardown signal (Kill/Delete). Non-sandbox workloads retain
		// the original behaviour.
		if !c.cType.IsCriSandbox() {
			c.signalExit()
		}
	}

	go waitContainerExit(ctx, s, c)

	return nil
}

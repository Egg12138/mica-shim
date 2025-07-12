package main

import (
	"context"
	tasksvc "mica-shim/core/taskService"
	log "mica-shim/logger"
	"os/signal"
	"syscall"

	"github.com/containerd/containerd/runtime/v2/shim"
)

var ShimName string

func main() {
	log.CleanDebugFile()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	tasksvc.RegisterPlugin()

	// NOTICE: as we consider, the next edition of containerd we focus on is containerd 2.x
	// according to the comments in containerd 1.7.27, shim.Run and shim.RunManager are removed
	// Hence we do not need to do a huge workload to support the new shim interface
	// Use RunManager for backwards compatibility with existing Manager interface
	shim.RunManager(ctx, tasksvc.NewManager(ShimName))
}

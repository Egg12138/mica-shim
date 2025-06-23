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
	// init and execute the shim
	// FUTURE (containerd 2.0) use latest shim.Run
	// 1.7.1-0.20230727135123-81895d22c9ee and later, the shim.Run parameters are changed
	shim.Run(ctx, tasksvc.NewManager(ShimName))
}

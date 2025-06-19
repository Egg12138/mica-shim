package main

import (
	"context"
	"mica-shim/core"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"os/signal"
	"syscall"

	"github.com/containerd/containerd/runtime/v2/shim"
)

func main() {
	log.CleanDebugFile()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()


	core.RegisterPlugin()
	// init and execute the shim
	// FUTURE (containerd 2.0) use latest shim.Run
	// 1.7.1-0.20230727135123-81895d22c9ee and later, the shim.Run parameters are changed
	shim.Run(ctx, core.NewManager(defs.ShimName))
}


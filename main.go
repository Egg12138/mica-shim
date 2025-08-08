package main

import (
	"context"
	"os/signal"
	"syscall"

	log "mica-shim/logger"
	tasksvc "mica-shim/pkg/entry"
	"os"

	"github.com/containerd/containerd/runtime/v2/shim"
)

var ShimName string

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if notTaskRequest() {
		os.Exit(0)
	}
	if err := log.CleanDebugFile(); err != nil {
		log.Errorf("failed to clean debug file: %v", err)
	}

	tasksvc.RegisterPlugin()
	log.Debugf("args: %s", os.Args)
	log.Debugf("tasksvc registered")

	// NOTICE: as we consider, the next edition of containerd we focus on is containerd 2.x
	// according to the comments in containerd 1.7.27, shim.Run and shim.RunManager are removed
	// Hence we do not need to do a not-trivial workload to support the new shim interface
	// Use RunManager for backwards compatibility with existing Manager interface
	shim.RunManager(ctx, tasksvc.NewManager(ShimName))
}

func notTaskRequest() bool {
	if len(os.Args) == 1 {
		return true
	}
	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "--version" || arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

package main

import (
	"mica-shim/pkg/shim"
	"os"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

// injected in Makefile
var ShimName string

func main() {

	if notTaskRequest() {
		os.Exit(0)
	}

	shimv2.Run(ShimName, shim.New, noReaper, noSubreaper, setupLogger)
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

func noReaper(c *shimv2.Config) {
	c.NoReaper = true
}

func noSubreaper(c *shimv2.Config) {
	c.NoSubreaper = true
}

func setupLogger(c *shimv2.Config) {
	c.NoSetupLogger = false
}

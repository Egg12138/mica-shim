// go:build linux
package main

import (
	"mica-shim/core"
	defs "mica-shim/definitions"

	"github.com/containerd/containerd/runtime/v2/shim"
)

func main() {
	// init and execute the shim
	// FUTURE (containerd 2.0) use latest shim.Run
	shim.Run(defs.ShimName, core.New)
}


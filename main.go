package main

import (
    "io"
    log "mica-shim/logger"
    "mica-shim/pkg/shim"
    "os"

    shimv2 "github.com/containerd/containerd/runtime/v2/shim"
    "github.com/sirupsen/logrus"
)

// ShimName injected in Makefile.
var ShimName string

func main() {
    if err := log.CleanDebugFile(); err != nil {
        log.Errorf("failed to clean debug file: %v", err)
    }

    if isBootstrapStart() {
        // During bootstrap "start", containerd reads CombinedOutput from the shim
        // for a strict JSON/address handshake. Any stderr/stdout noise corrupts
        // the handshake and leaves the shim socket file behind. Silence console
        // output and rely on our debug file for diagnostics in this phase.
        log.Log.SetLevel(logrus.WarnLevel)
        log.Log.SetOutput(io.Discard)
    }

	log.Debugf("main() called, checking if task request")

	if notTaskRequest() {
		os.Exit(0)
	}

	shimv2.Run(ShimName, shim.New, noReaper, noSubreaper, setupLogger)
	// Avoid noisy info log after start handshake; keep at debug level.
	log.Debugf("shimv2.Run() returned normally")
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

func isBootstrapStart() bool {
	for _, arg := range os.Args[1:] {
		if arg == "start" {
			return true
		}
	}
	return false
}

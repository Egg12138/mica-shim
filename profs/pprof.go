package main

import (
	"log"
	"net/http"
	_ "net/http/pprof" // registers pprof handlers
	"os"
)

// init starts an HTTP pprof server if the environment variable MICA_SHIM_PPROF is set.
// The default listen address is "localhost:6060", but it can be overridden by
// setting MICA_SHIM_PPROF_ADDR to a different host:port combination.
func init() {
	if os.Getenv("MICA_SHIM_PPROF") == "" {
		return
	}

	addr := os.Getenv("MICA_SHIM_PPROF_ADDR")
	if addr == "" {
		addr = "localhost:6060"
	}

	go func() {
		log.Printf("[profiler] pprof HTTP server listening on %s (set MICA_SHIM_PPROF_ADDR to change)\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("[profiler] pprof HTTP server error: %v", err)
		}
	}()
}

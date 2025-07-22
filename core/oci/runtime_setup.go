package core

import log "mica-shim/logger"

type RuntimeConfig struct {
	Debug bool
	SandboxCPUs uint32
	SandboxMemMB uint32
}

func NewRuntimeSpec() *RuntimeConfig {
	spec := RuntimeConfig{
		Debug: false,
		SandboxCPUs: 0,
		SandboxMemMB: 0,
	}
	return &spec
}

func (r *RuntimeConfig) SetDebug(debug bool) {
	log.Debugf("Setting debug to %v", debug)
	r.Debug = debug
}

func (r *RuntimeConfig) SetSandboxCPUs(cpu uint32) {
	log.Debugf("Setting sandbox CPUs to %d", cpu)
	r.SandboxCPUs = cpu
}

func (r *RuntimeConfig) SetSandboxMemMB(mem uint32) {
	log.Debugf("Setting sandbox memory to %dMB", mem)
	r.SandboxMemMB = mem
}

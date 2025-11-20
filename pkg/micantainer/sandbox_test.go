package micantainer

import (
	"testing"

	ped "mica-shim/pkg/pedestal"
)

func TestSandboxConfigValidInfraOnly(t *testing.T) {
	cfg := SandboxConfig{
		ID: "infra-sandbox",
		PedConfig: ped.PedestalConfig{
			PedType: ped.Unsupported,
		},
		ContainerConfigs: map[string]*ContainerConfig{},
		InfraOnly:        true,
	}

	if !cfg.valid() {
		t.Fatalf("expected infra-only sandbox with unsupported pedestal to be accepted")
	}

	cfg.InfraOnly = false
	if cfg.valid() {
		t.Fatalf("expected sandbox without pedestal support to be rejected when not infra-only")
	}
}

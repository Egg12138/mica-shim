//go:build test
// +build test

package shim

import (
	defs "mica-shim/definitions"
	oci "mica-shim/pkg/oci"
	pedestal "mica-shim/pkg/pedestal"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverMicrunConfigFilesEnvOverride(t *testing.T) {
	t.Setenv(defs.MicrunConfDirEnv, "")
	t.Setenv(defs.MicrunConfEnv, "")

	dir := t.TempDir()
	override := filepath.Join(dir, "override.ini")
	require.NoError(t, os.WriteFile(override, []byte(""), 0644))

	t.Setenv(defs.MicrunConfEnv, override)

	files, err := discoverMicrunConfigFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, override, files[0].Path)
	require.Equal(t, formatINI, files[0].Format)
}

func TestDiscoverMicrunConfigFilesDirOrdered(t *testing.T) {
	t.Setenv(defs.MicrunConfEnv, "")

	dir := t.TempDir()
	b := filepath.Join(dir, "b.ini")
	a := filepath.Join(dir, "a.ini")
	require.NoError(t, os.WriteFile(b, []byte(""), 0644))
	require.NoError(t, os.WriteFile(a, []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte(""), 0644))

	t.Setenv(defs.MicrunConfDirEnv, dir)

	files, err := discoverMicrunConfigFiles()
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, a, files[0].Path)
	require.Equal(t, b, files[1].Path)
}

func TestDiscoverMicrunConfigFilesDefaultFallback(t *testing.T) {
	t.Setenv(defs.MicrunConfEnv, "")
	t.Setenv(defs.MicrunConfDirEnv, "")

	dir := t.TempDir()
	fallback := filepath.Join(dir, "micrun.ini")
	require.NoError(t, os.WriteFile(fallback, []byte(""), 0644))

	origDropins := defaultDropinSearch
	origFiles := defaultConfigFiles
	defaultDropinSearch = nil
	defaultConfigFiles = []string{fallback}
	defer func() {
		defaultDropinSearch = origDropins
		defaultConfigFiles = origFiles
	}()

	files, err := discoverMicrunConfigFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, fallback, files[0].Path)
}

func TestApplyMicrunConfigFilesFullConfig(t *testing.T) {
	defer pedestal.SetExclusiveDom0CPU(false)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "micrun.ini")
	content := `
[static_resource]
static_resource = false

[debug]
debug = true

[pause_image]
pause_image = registry.test/pause:2.0

[max_container_vcpu]
max_container_vcpu = 6

[sandbox_minimum_vcpu]
sandbox_minimum_vcpu = 3

[hugepage_enable]
hugepage_enable = true

[exclusive_dom0_cpu]
exclusive_dom0_cpu = true

[firmware_path]
firmware_path = /opt/fw/firmware.elf
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	cfg := oci.NewRuntimeConfig()
	cfg.StaticResourceManagement = true // ensure change is visible
	applyMicrunConfigFiles(cfg, []micrunConfigFile{{Path: configPath, Format: formatINI}})

	require.False(t, cfg.StaticResourceManagement, "static_resource should be false")
	require.True(t, cfg.Debug, "debug should be true")
	require.Equal(t, "registry.test/pause:2.0", cfg.PauseImage)
	require.Equal(t, uint32(6), cfg.MaxContainerCPUs)
	require.Equal(t, uint32(3), cfg.MiniVCPUNum)
	require.True(t, cfg.HugePageSupport)
	require.True(t, cfg.ExclusiveDom0CPU)
	require.True(t, pedestal.ExclusiveDom0CPUEnabled())
	require.Equal(t, "/opt/fw/firmware.elf", cfg.DefaultFirmwarePath)
}

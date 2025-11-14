package shim

import (
	defs "mica-shim/definitions"
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

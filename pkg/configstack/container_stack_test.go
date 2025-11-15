//go:build test
// +build test

package configstack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClientLayer(t *testing.T) {
	dir := t.TempDir()
	conf := `[Mica]
OS=rtos
Pedestal= xen
PedestalConf=firmware.bin
ClientPath = images/app.elf
`
	if err := os.WriteFile(filepath.Join(dir, "client.conf"), []byte(conf), 0o644); err != nil {
		t.Fatalf("failed to write client.conf: %v", err)
	}

	layer, err := LoadClientLayer(dir)
	if err != nil {
		t.Fatalf("LoadClientLayer returned error: %v", err)
	}
	if layer.OS != "rtos" {
		t.Fatalf("expected OS override 'rtos', got %q", layer.OS)
	}
	if layer.PedestalType != "xen" {
		t.Fatalf("expected pedestal type xen, got %q", layer.PedestalType)
	}
	if layer.PedestalConf != "firmware.bin" {
		t.Fatalf("expected pedestal conf 'firmware.bin', got %q", layer.PedestalConf)
	}
	if layer.FirmwarePath != "images/app.elf" {
		t.Fatalf("expected firmware path 'images/app.elf', got %q", layer.FirmwarePath)
	}
}

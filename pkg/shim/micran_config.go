package shim

import (
	"errors"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"mica-shim/pkg/oci"
	"mica-shim/pkg/utils"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type configFormat int

const (
	formatUnknown configFormat = iota
	formatINI
	formatTOML
)

type micrunConfigFile struct {
	Path   string
	Format configFormat
}

var (
	defaultDropinSearch = []string{defs.MicrunConfDropin}
	defaultConfigFiles  = []string{filepath.Join(defs.MicrunConfDir, defs.DefaultMicrunConf)}
)

func discoverMicrunConfigFiles() ([]micrunConfigFile, error) {
	if override := firstNonEmptyEnv(defs.MicrunConfEnv); override != "" {
		f, err := makeConfigFile(override)
		if err != nil {
			return nil, err
		}
		return []micrunConfigFile{f}, nil
	}

	if dir := firstNonEmptyEnv(defs.MicrunConfDirEnv); dir != "" {
		files, err := listMicrunConfigDir(dir)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			return files, nil
		}
	}

	var aggregated []micrunConfigFile
	for _, dir := range defaultDropinSearch {
		files, err := listMicrunConfigDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		aggregated = append(aggregated, files...)
	}
	if len(aggregated) > 0 {
		return aggregated, nil
	}

	for _, candidate := range defaultConfigFiles {
		f, err := makeConfigFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return []micrunConfigFile{f}, nil
	}

	return nil, nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func makeConfigFile(path string) (micrunConfigFile, error) {
	if !utils.IsRegular(path) {
		return micrunConfigFile{}, fmt.Errorf("micrun config %s is not a regular file or failed to stat it", path)
	}
	format := detectConfigFormat(path)
	if format == formatUnknown {
		return micrunConfigFile{}, fmt.Errorf("unsupported micrun config format: %s", path)
	}
	return micrunConfigFile{Path: path, Format: format}, nil
}

func listMicrunConfigDir(dir string) ([]micrunConfigFile, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []micrunConfigFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		format := detectConfigFormat(full)
		if format == formatUnknown {
			continue
		}
		files = append(files, micrunConfigFile{Path: full, Format: format})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func detectConfigFormat(path string) configFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ini":
		return formatINI
	case ".toml":
		return formatTOML
	default:
		return formatUnknown
	}
}

func applyMicrunConfigFiles(cfg *oci.RuntimeConfig, files []micrunConfigFile) {
	for _, file := range files {
		if err := applyMicrunConfigFile(cfg, file); err != nil {
			log.Warnf("failed to apply micrun config %s: %v", file.Path, err)
		}
	}
}

func applyMicrunConfigFile(cfg *oci.RuntimeConfig, file micrunConfigFile) error {
	switch file.Format {
	case formatINI:
		log.Debugf("loading micrun config: %s", file.Path)
		return cfg.ParseRuntimeFromFile(file.Path)
	default:
		return fmt.Errorf("config format %v not supported yet", file.Format)
	}
}

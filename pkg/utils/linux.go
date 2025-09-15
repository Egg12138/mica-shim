package utils

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// KoLoaded reports whether the named kernel module is present in the running kernel.
// It reads /proc/modules once and caches the parsed list for subsequent calls.
func KoLoaded(name string) (bool, error) {
	staticList, err := loadKoList()
	if err != nil {
		return false, err
	}
	_, ok := staticList[name]
	return ok, nil
}

// loadList parses /proc/modules exactly once and returns the set of loaded modules.
// sync.Once guarantees the file is read only one time
var (
	loaded   map[string]struct{} // cached module names
	loadOnce sync.Once
	loadErr  error // capture the first error, if any
)

func loadKoList() (map[string]struct{}, error) {
	loadOnce.Do(func() {
		f, err := os.Open("/proc/modules")
		if err != nil {
			loadErr = fmt.Errorf("cannot open /proc/modules: %w", err)
			return
		}
		defer f.Close()

		loaded = make(map[string]struct{})
		sc := bufio.NewScanner(f)

		// Each line: "modulename size refs deps state addr"
		// We only need the first token
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) == 0 {
				continue
			}
			loaded[fields[0]] = struct{}{}
		}
		loadErr = sc.Err()
	})
	return loaded, loadErr
}

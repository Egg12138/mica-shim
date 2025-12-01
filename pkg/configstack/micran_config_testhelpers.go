//go:build test
// +build test

package configstack

// OverrideDefaultDropinSearch temporarily replaces the default drop-in directories.
func OverrideDefaultDropinSearch(paths []string) func() {
	orig := defaultDropinSearch
	if paths == nil {
		defaultDropinSearch = nil
	} else {
		clone := make([]string, len(paths))
		copy(clone, paths)
		defaultDropinSearch = clone
	}
	return func() {
		defaultDropinSearch = orig
	}
}

// OverrideDefaultConfigFile temporarily changes the fallback config file path.
func OverrideDefaultConfigFile(path string) func() {
	orig := defaultConfigFile
	defaultConfigFile = path
	return func() {
		defaultConfigFile = orig
	}
}

package shim

import (
	"reflect"
)

// getConfigPathFromOldCRI extracts ConfigPath from legacy containerd CRI option structs
// without requiring optional build tags. When the incoming message exposes either a
// GetConfigPath method or a ConfigPath string field, the path is returned.
func getConfigPathFromOldCRI(v any) (string, bool) {
	if v == nil {
		return "", false
	}

	type configPathGetter interface {
		GetConfigPath() string
	}
	if getter, ok := v.(configPathGetter); ok {
		if path := getter.GetConfigPath(); path != "" {
			return path, true
		}
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return "", false
	}

	field := rv.FieldByName("ConfigPath")
	if field.IsValid() && field.Kind() == reflect.String {
		if path := field.String(); path != "" {
			return path, true
		}
	}
	return "", false
}

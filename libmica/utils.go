package libmica

import (
	defs "mica-shim/definitions"
	"strings"
)

func startWithMicaPrefix(fieldName string) bool {
	if strings.HasPrefix(fieldName, defs.MicaAnnotationPrefix) {
		return true
	} else {
		return false
	}
}

func isMicaAnnotation(fieldName string) string {
	return strings.TrimPrefix(fieldName, defs.MicaAnnotationPrefix)
}

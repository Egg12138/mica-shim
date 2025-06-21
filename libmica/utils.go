package libmica

import (
	defs "mica-shim/definitions"
	"strings"
)

func StartWithMicaPrefix(fieldName string) bool {
	if strings.HasPrefix(fieldName, defs.MicaAnnotationPrefix) {
		return true
	} else {
		return false
	}
}

func IsMicaAnnotation(fieldName string) string {
	return strings.TrimPrefix(fieldName, defs.MicaAnnotationPrefix)
}

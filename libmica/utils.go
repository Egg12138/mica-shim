package libmica

import (
	defs "mica-shim/definitions"
	"strings"
)

func startWithMicaPrefix(fieldName string) bool {
	return strings.HasPrefix(fieldName, defs.MicaLabelPrefix)
}

func isMicaAnnotation(fieldName string) string {
	return strings.TrimPrefix(fieldName, defs.MicaLabelPrefix)
}
package libmica

import (
	defs "mica-shim/definitions"
	"strings"
)

// Helper functions
func startWithMicaPrefix(fieldName string) bool {
	return strings.HasPrefix(fieldName, defs.MicraLabelPrefix)
}

func isMicaAnnotation(fieldName string) string {
	return strings.TrimPrefix(fieldName, defs.MicraLabelPrefix)
}

//go:build test
// +build test

package libmica

// OverrideMicaCtlForTest temporarily swaps the micaCtl handler.
func OverrideMicaCtlForTest(fn micaCtlFunc) func() {
	previous := micaCtlFn
	if fn == nil {
		micaCtlFn = micaCtlImpl
	} else {
		micaCtlFn = fn
	}
	return func() {
		micaCtlFn = previous
	}
}

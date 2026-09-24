package kyvernopolicy

import (
	"sync"

	"github.com/go-logr/logr"
)

// ReplaceControllerRuntimeLoggerSetter swaps the SetLogger seam and re-arms the
// once-per-process guard, returning a function that restores both. Callers must
// not run in parallel with other tests.
func ReplaceControllerRuntimeLoggerSetter(setter func(logr.Logger)) func() {
	originalSetter := setControllerRuntimeLogger
	originalOnce := silenceOnce
	setControllerRuntimeLogger = setter
	silenceOnce = new(sync.Once)

	return func() {
		setControllerRuntimeLogger = originalSetter
		silenceOnce = originalOnce
	}
}

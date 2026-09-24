package kyvernopolicy

import (
	"sync"

	"github.com/go-logr/logr"
)

// ReplaceControllerRuntimeLoggerSetter swaps the SetLogger seam and re-arms the
// once-per-process guard, returning a function restoring the original setter.
// Callers must not run in parallel with other tests.
func ReplaceControllerRuntimeLoggerSetter(setter func(logr.Logger)) func() {
	original := setControllerRuntimeLogger
	setControllerRuntimeLogger = setter
	silenceOnce = sync.Once{}

	return func() { setControllerRuntimeLogger = original }
}

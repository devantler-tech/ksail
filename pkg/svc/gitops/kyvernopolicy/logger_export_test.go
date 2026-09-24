package kyvernopolicy

import "github.com/go-logr/logr"

// ReplaceControllerRuntimeLoggerSetter swaps the SetLogger seam and returns a
// function restoring the original.
func ReplaceControllerRuntimeLoggerSetter(setter func(logr.Logger)) func() {
	original := setControllerRuntimeLogger
	setControllerRuntimeLogger = setter

	return func() { setControllerRuntimeLogger = original }
}

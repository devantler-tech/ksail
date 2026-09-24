package kyvernopolicy

import (
	"github.com/go-logr/logr"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// setControllerRuntimeLogger is controller-runtime's SetLogger, held in a
// variable so a test can observe the call.
//
//nolint:gochecknoglobals // test seam for a process-wide logger
var setControllerRuntimeLogger = ctrllog.SetLogger

// silenceControllerRuntimeLogger gives controller-runtime's global logger a
// sink that discards everything. Kyverno's engine logs through that logger,
// and when nothing has set it controller-runtime prints a "log.SetLogger(...)
// was never called" warning with a full goroutine stack once the process is
// 30 seconds old, which a long validate run always is (issue #7255).
//
// Only the first SetLogger call in a process takes effect, so this never
// replaces a logger the operator has already configured.
func silenceControllerRuntimeLogger() {
	setControllerRuntimeLogger(logr.New(ctrllog.NullLogSink{}))
}

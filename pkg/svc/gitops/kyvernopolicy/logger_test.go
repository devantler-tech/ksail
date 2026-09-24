package kyvernopolicy_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnginesSilenceControllerRuntimeLogger pins issue #7255: Kyverno logs
// through controller-runtime's global logger, and when nothing sets it
// controller-runtime prints a "log.SetLogger(...) was never called" warning with
// a goroutine stack during a long validate run. Both engines must set a
// discarding logger when they are built.
//
//nolint:paralleltest // swaps a package-level seam
func TestEnginesSilenceControllerRuntimeLogger(t *testing.T) {
	for name, build := range map[string]func(t *testing.T){
		"classic": func(t *testing.T) {
			t.Helper()

			kyvernopolicy.NewEngine(nil, nil)
		},
		"CEL": func(t *testing.T) {
			t.Helper()

			_, err := kyvernopolicy.NewCELEngine(nil, nil)
			require.NoError(t, err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var loggers []logr.Logger

			restore := kyvernopolicy.ReplaceControllerRuntimeLoggerSetter(func(l logr.Logger) {
				loggers = append(loggers, l)
			})
			defer restore()

			build(t)

			require.Len(t, loggers, 1, "the engine must set controller-runtime's logger")
			assert.False(t, loggers[0].Enabled(), "the logger must discard everything")
		})
	}
}

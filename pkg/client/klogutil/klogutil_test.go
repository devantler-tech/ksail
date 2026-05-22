package klogutil_test

import (
	"flag"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/klogutil"
	"github.com/stretchr/testify/require"
)

// TestSilence verifies that Silence configures klog flags so client-go log
// messages are suppressed and do not leak into KSail command output.
//
//nolint:paralleltest // Mutates process-global flag.CommandLine state.
func TestSilence(t *testing.T) {
	// Not parallel: mutates process-global flag state.
	klogutil.Silence()

	cases := map[string]string{
		"logtostderr":     "false",
		"alsologtostderr": "false",
		// klog's stderrthreshold is a severity value; "fatal" stringifies to "3".
		"stderrthreshold": "3",
		"v":               "-10",
	}
	for name, want := range cases {
		f := flag.Lookup(name)
		require.NotNilf(t, f, "expected klog flag %q to be registered", name)
		require.Equalf(t, want, f.Value.String(), "flag %q value mismatch", name)
	}
}

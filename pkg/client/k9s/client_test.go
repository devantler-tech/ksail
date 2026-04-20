package k9s_test

import (
	"flag"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/k9s"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	client := k9s.NewClient()
	require.NotNil(t, client, "expected client to be created")
}

func TestCreateConnectCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		kubeConfigPath string
		context        string
	}{
		{
			name:           "with kubeconfig path",
			kubeConfigPath: "/path/to/kubeconfig",
			context:        "",
		},
		{
			name:           "without kubeconfig path",
			kubeConfigPath: "",
			context:        "",
		},
		{
			name:           "with context",
			kubeConfigPath: "/path/to/kubeconfig",
			context:        "my-context",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := k9s.NewClient()
			cmd := client.CreateConnectCommand(testCase.kubeConfigPath, testCase.context)

			require.NotNil(t, cmd, "expected command to be created")
			require.Equal(t, "connect", cmd.Use, "expected Use to be 'connect'")
			require.Equal(t, "Connect to cluster with k9s", cmd.Short, "expected Short description")
			require.Contains(
				t,
				cmd.Long,
				"Launch k9s terminal UI",
				"expected Long description to mention k9s",
			)
			require.True(t, cmd.SilenceUsage, "expected SilenceUsage to be true")
		})
	}
}

func TestCreateConnectCommandStructure(t *testing.T) {
	t.Parallel()

	client := k9s.NewClient()
	cmd := client.CreateConnectCommand("", "")

	// Verify RunE is set
	require.NotNil(t, cmd.RunE, "expected RunE to be set")

	// NOTE: We cannot execute RunE in unit tests because it launches k9s.
}

// TestSilenceKlog verifies that invoking the k9s client's klog-silencing
// helper (exposed via the exported test seam) configures klog flags so
// client-go log messages do not leak onto the k9s TUI. This is the fix
// for the garbled `ksail cluster connect` output caused by klog writing
// to stderr while the alternate screen is active.
//
//nolint:paralleltest // Mutates process-global flag.CommandLine state.
func TestSilenceKlog(t *testing.T) {
	// Not parallel: mutates process-global flag state.
	k9s.SilenceKlogForTest()

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

package kubectl_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/stretchr/testify/require"
)

// TestWithKubeContextPinsContextFlag verifies that the context passed to
// WithKubeContext flows all the way through to the --context flag of the
// commands the client builds, and that an empty context is a no-op (the
// command falls back to the kubeconfig's current-context).
func TestWithKubeContextPinsContextFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		context  string
		wantFlag string
	}{
		{name: "named context is pinned", context: "prod-cluster", wantFlag: "prod-cluster"},
		{name: "empty context is a no-op", context: "", wantFlag: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := kubectl.NewClient(createTestIOStreams()).WithKubeContext(test.context)

			cmd := client.CreateApplyCommand("")

			flag := cmd.Flags().Lookup("context")
			require.NotNil(t, flag, "expected built command to register a --context flag")
			require.Equal(
				t,
				test.wantFlag,
				flag.Value.String(),
				"--context flag value should match the pinned context",
			)
		})
	}
}

// TestWithKubeContextReturnsIndependentCopy verifies that WithKubeContext does
// not mutate the receiver: it returns a distinct client, and pinning a context
// on the copy leaves the original building commands with no --context set.
func TestWithKubeContextReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	original := kubectl.NewClient(createTestIOStreams())

	pinned := original.WithKubeContext("staging")

	require.NotSame(
		t,
		original,
		pinned,
		"WithKubeContext should return a distinct client, not the receiver",
	)

	// The original must be unaffected by the copy's pinned context.
	originalFlag := original.CreateApplyCommand("").Flags().Lookup("context")
	require.NotNil(t, originalFlag)
	require.Empty(
		t,
		originalFlag.Value.String(),
		"the original client should keep building commands with no pinned --context",
	)

	// Re-pinning the original yields yet another independent client whose own
	// context is set, without disturbing the first copy.
	repinned := original.WithKubeContext("dev")
	require.NotSame(t, pinned, repinned)

	pinnedFlag := pinned.CreateApplyCommand("").Flags().Lookup("context")
	require.NotNil(t, pinnedFlag)
	require.Equal(t, "staging", pinnedFlag.Value.String())

	repinnedFlag := repinned.CreateApplyCommand("").Flags().Lookup("context")
	require.NotNil(t, repinnedFlag)
	require.Equal(t, "dev", repinnedFlag.Value.String())
}

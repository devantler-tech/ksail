package confirm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/confirm"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest,tparallel // Subtests cannot run in parallel - they share TTY checker state
func TestShouldSkipPrompt(t *testing.T) {

	tests := []struct {
		name     string
		force    bool
		isTTY    bool
		expected bool
	}{
		{
			name:     "force_true_skips_prompt",
			force:    true,
			isTTY:    true,
			expected: true,
		},
		{
			name:     "force_true_non_tty_skips_prompt",
			force:    true,
			isTTY:    false,
			expected: true,
		},
		{
			name:     "non_tty_skips_prompt",
			force:    false,
			isTTY:    false,
			expected: true,
		},
		{
			name:     "tty_without_force_shows_prompt",
			force:    false,
			isTTY:    true,
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Do NOT run subtests in parallel - they share TTY checker state
			restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return testCase.isTTY })
			defer restoreTTY()

			result := confirm.ShouldSkipPrompt(testCase.force)
			require.Equal(t, testCase.expected, result)
		})
	}
}

// promptTestCase is a test case for PromptForConfirmation.
type promptTestCase struct {
	name     string
	input    string
	expected bool
}

// getPromptTestCases returns test cases for PromptForConfirmation.
func getPromptTestCases() []promptTestCase {
	return []promptTestCase{
		{"yes_lowercase_confirms", "yes\n", true},
		{"yes_uppercase_confirms", "YES\n", true},
		{"yes_mixed_case_confirms", "Yes\n", true},
		{"no_denies", "no\n", false},
		{"y_denies", "y\n", false},
		{"empty_denies", "\n", false},
		{"random_text_denies", "maybe\n", false},
	}
}

//nolint:paralleltest,tparallel // Subtests cannot run in parallel - they share stdin reader state
func TestPromptForConfirmation(t *testing.T) {

	for _, testCase := range getPromptTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			// Do NOT run subtests in parallel - they share stdin reader state
			restoreStdin := confirm.SetStdinReaderForTests(strings.NewReader(testCase.input))
			defer restoreStdin()

			var out bytes.Buffer

			result := confirm.PromptForConfirmation(&out)

			require.Equal(t, testCase.expected, result)
			// PromptForConfirmation no longer writes output - prompt is in ShowDeletionPreview
			require.Empty(t, out.String())
		})
	}
}

func TestShowDeletionPreview_Docker(t *testing.T) {
	t.Parallel()

	preview := &confirm.DeletionPreview{
		ClusterName: "my-cluster",
		Provider:    v1alpha1.ProviderDocker,
		Nodes:       []string{"my-cluster-control-plane", "my-cluster-worker"},
		Registries:  []string{"registry.localhost"},
	}

	var out bytes.Buffer
	confirm.ShowDeletionPreview(&out, preview)

	output := out.String()
	require.Contains(t, output, "The following resources will be deleted")
	require.Contains(t, output, "my-cluster")
	require.Contains(t, output, "Docker")
	require.Contains(t, output, "Containers:")
	require.Contains(t, output, "my-cluster-control-plane")
	require.Contains(t, output, "my-cluster-worker")
	require.Contains(t, output, "Registries:")
	require.Contains(t, output, "registry.localhost")
	require.Contains(t, output, `Type "yes" to confirm deletion`)
}

func TestShowDeletionPreview_DockerNoResources(t *testing.T) {
	t.Parallel()

	preview := &confirm.DeletionPreview{
		ClusterName: "empty-cluster",
		Provider:    v1alpha1.ProviderDocker,
	}

	var out bytes.Buffer
	confirm.ShowDeletionPreview(&out, preview)

	output := out.String()
	require.Contains(t, output, "empty-cluster")
	require.Contains(t, output, "Docker")
	require.Contains(t, output, `Type "yes" to confirm deletion`)
	// Should not contain resource sections when empty
	require.NotContains(t, output, "Containers:")
	require.NotContains(t, output, "Registries:")
}

func TestShowDeletionPreview_Hetzner(t *testing.T) {
	t.Parallel()

	preview := &confirm.DeletionPreview{
		ClusterName:    "prod-cluster",
		Provider:       v1alpha1.ProviderHetzner,
		Servers:        []string{"prod-cluster-cp-1", "prod-cluster-worker-1"},
		PlacementGroup: "prod-cluster-placement",
		Firewall:       "prod-cluster-firewall",
		Network:        "prod-cluster-network",
	}

	var out bytes.Buffer
	confirm.ShowDeletionPreview(&out, preview)

	output := out.String()
	require.Contains(t, output, "prod-cluster")
	require.Contains(t, output, "Hetzner")
	require.Contains(t, output, "Servers:")
	require.Contains(t, output, "prod-cluster-cp-1")
	require.Contains(t, output, "prod-cluster-worker-1")
	require.Contains(t, output, "Placement Group: prod-cluster-placement")
	require.Contains(t, output, "Firewall: prod-cluster-firewall")
	require.Contains(t, output, "Network: prod-cluster-network")
	require.Contains(t, output, `Type "yes" to confirm deletion`)
}

func TestIsTTY_Override(t *testing.T) {
	t.Parallel()

	// Test that override works
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return true })

	require.True(t, confirm.IsTTY())

	restoreTTY()

	// Test the opposite
	restoreTTY = confirm.SetTTYCheckerForTests(func() bool { return false })

	require.False(t, confirm.IsTTY())

	restoreTTY()
}

func TestErrDeletionCancelled(t *testing.T) {
	t.Parallel()

	require.Error(t, confirm.ErrDeletionCancelled)
	require.Equal(t, "deletion cancelled", confirm.ErrDeletionCancelled.Error())
}

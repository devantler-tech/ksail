package analysisrunner_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPreflightPolicyChangesRunTheirAcceptanceChecks(t *testing.T) {
	t.Parallel()

	workflow := readWorkflow(t)
	for _, event := range []string{"pull_request", "push"} {
		require.Contains(t, value(t, workflow, "on", event, "paths"),
			".github/workflows/verify-ksail-arc-delivery.yaml", event)
	}
}

func TestARCPreflightRequiresExplicitMainBranchEnablement(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile("../../.github/workflows/verify-ksail-arc-delivery.yaml")
	require.NoError(t, err)

	var workflow map[string]any
	require.NoError(t, yaml.Unmarshal(content, &workflow))
	require.Empty(t, value(t, workflow, "permissions"))
	require.Len(t, value(t, workflow, "on"), 1, "no automatic ARC job triggers")
	require.Equal(
		t,
		"boolean",
		value(t, workflow, "on", "workflow_dispatch", "inputs", "enable_arc", "type"),
	)
	require.Equal(
		t,
		false,
		value(t, workflow, "on", "workflow_dispatch", "inputs", "enable_arc", "default"),
	)
	require.Equal(t, false, value(t, workflow, "concurrency", "cancel-in-progress"))

	job := value(t, workflow, "jobs", "preflight")
	require.Equal(t, "github.ref == 'refs/heads/main' && inputs.enable_arc", value(t, job, "if"))
	require.Equal(t, "ksail-code-quality", value(t, job, "runs-on"))
	require.Equal(t, 15, value(t, job, "timeout-minutes"))
	require.Equal(t, map[string]any{"contents": "read"}, value(t, job, "permissions"))

	pinned := regexp.MustCompile(`@[0-9a-f]{40}$`)

	var liveSmoke bool

	for _, step := range steps(t, job) {
		if uses, ok := step["uses"]; ok {
			require.Regexp(t, pinned, uses)
		}

		if with, ok := step["with"].(map[string]any); ok {
			if _, checkout := with["persist-credentials"]; checkout {
				require.Equal(t, false, with["persist-credentials"])
				require.Equal(t, "${{ github.sha }}", with["ref"])
			}
		}

		if run, ok := step["run"].(string); ok {
			require.NotContains(t, run, "sudo")
			require.NotContains(t, run, "docker")
			require.NotContains(t, run, "secrets.")

			if run == "/usr/local/bin/ksail-analysis-smoke --live-runner" {
				liveSmoke = true
			}
		}
	}

	require.True(
		t,
		liveSmoke,
		"an actual ARC job must exercise the image without rewriting runner credentials",
	)
}

package ciharness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A successful fixture is insufficient: both orderings must run under the native
// Actions engine, and the required gate must consume the real artifact verdict.
func TestDiagnosticsArtifactProbeIsBlockingAndIncludesCounterfactual(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	job, ok := workflow.Jobs["system-test-diagnostics-artifact"]
	require.True(t, ok, "missing native diagnostics artifact probe")
	assert.False(t, job.ContinueOnError)
	assert.Equal(t, 10, job.TimeoutMinutes)
	assert.Equal(t, []string{"changes"}, job.Needs)
	assert.Equal(t, workflow.Jobs["verify-todos-workflow-contract"].If, job.If)
	assert.Equal(t, map[string]string{"contents": "read", "actions": "read"}, job.Permissions)
	assert.Equal(t, false, job.Strategy["fail-fast"])
	matrix := requireStringMap(t, job.Strategy["matrix"])
	assert.ElementsMatch(t, []any{"baseline", "candidate"}, requireAnySlice(t, matrix["mode"]))

	for _, mode := range []string{"baseline", "candidate"} {
		probe := findHarnessStep(t, job.Steps, "Exercise "+mode+" failure and artifact programs")
		assert.Equal(t, "probe-"+mode, probe.ID)
		assert.Equal(t, "matrix.mode == '"+mode+"'", probe.If)
		assert.True(t, probe.ContinueOnError, "the verifier must examine the intended failure")
		assert.Equal(t, "$/.github/actions/diagnostics-artifact-"+mode, probe.Uses,
			"the native engine must load the immutable reviewed fixture")
		assert.Equal(t, map[string]any{
			"distribution": "Vanilla", "provider": "Hetzner",
			"test-autoscaler-inventory": "true", "upload-artifacts": "true",
		}, probe.With, "Vanilla makes the real Talos trial refuse before any cloud operation")
	}

	verify := findHarnessStep(t, job.Steps, "Verify native outcomes and artifact identity")
	assert.Equal(t, "always()", verify.If)
	assert.False(t, verify.ContinueOnError)
	assert.Equal(t, "bash .github/scripts/verify-diagnostics-artifact.sh\n", verify.Run)

	for key, expression := range map[string]string{
		"PROBE_OUTCOME": "${{ matrix.mode == 'candidate' && " +
			"steps.probe-candidate.outcome || steps.probe-baseline.outcome }}",
		"TRIAL_OUTCOME": "${{ matrix.mode == 'candidate' && " +
			"steps.probe-candidate.outputs.trial-outcome || steps.probe-baseline.outputs.trial-outcome }}",
		"SAVE_OUTCOME": "${{ matrix.mode == 'candidate' && " +
			"steps.probe-candidate.outputs.save-outcome || steps.probe-baseline.outputs.save-outcome }}",
		"UPLOAD_OUTCOME": "${{ matrix.mode == 'candidate' && " +
			"steps.probe-candidate.outputs.upload-outcome || steps.probe-baseline.outputs.upload-outcome }}",
		"EXPECTED_HEAD": "${{ github.event.pull_request.head.sha || github.sha }}",
	} {
		assert.Equal(t, expression, verify.Env[key])
	}

	download := findHarnessStep(t, job.Steps, "Download the observed candidate artifact")
	assert.Equal(t, "matrix.mode == 'candidate'", download.If)
	assert.Contains(t, download.Uses, "actions/download-artifact@")
	assert.Contains(t, findHarnessStep(t, job.Steps, "Verify downloaded diagnostics content").Run,
		"KSail diagnostics artifact fixture")

	gate := workflow.Jobs["require-checks-in-pr"]
	assert.Contains(t, gate.Needs, "system-test-diagnostics-artifact")
	require.NotEmpty(t, gate.Steps)
	assert.Contains(t, gate.Steps[0].With["job-results"],
		"${{ needs.system-test-diagnostics-artifact.result }}")
}

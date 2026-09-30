package ciharness_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEKSUpgradeTrialRunsFailureControls exercises the actual trial driver with
// local command doubles, including a successful CLI that does not upgrade AWS.
func TestEKSUpgradeTrialRunsFailureControls(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"bash", "jq", "curl"} {
		requireTestExecutable(t, command)
	}

	cmd := exec.CommandContext(
		t.Context(),
		"bash",
		"../../.github/scripts/eks-upgrade-trial.test.sh",
	)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "EKS upgrade trial controls failed:\n%s", output)
	assert.Contains(t, string(output), "PASS: successful upgrade and repeat")
}

// TestEKSUpgradeTrialReservesFreshCleanupSession guards the long upgrade path:
// an upgrade timeout must leave both credentials and job time for deletion.
func TestEKSUpgradeTrialReservesFreshCleanupSession(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	job := workflow.Jobs["smoke-test"]
	trial := findHarnessStep(t, job.Steps, "🧪 EKS control-plane upgrade trial")
	assert.Equal(t, "${{ inputs.upgrade_from_version }}", trial.Env["EKS_UPGRADE_FROM"])
	assert.Equal(t, "${{ inputs.upgrade_to_version }}", trial.Env["EKS_UPGRADE_TO"])
	assert.Equal(t, "inputs.upgrade_from_version != ''", trial.If)
	assert.False(t, trial.ContinueOnError)
	assert.GreaterOrEqual(t, trial.TimeoutMinutes, 75)

	refresh := findHarnessStep(t, job.Steps, "🔐 Refresh AWS credentials for final cleanup")
	assert.Contains(t, refresh.If, "always()")
	assert.Equal(t, 7200, refresh.With["role-duration-seconds"])
	assert.Less(t,
		harnessStepIndex(t, job.Steps, trial.Name),
		harnessStepIndex(t, job.Steps, refresh.Name))
	assert.Less(t,
		harnessStepIndex(t, job.Steps, refresh.Name),
		harnessStepIndex(t, job.Steps, "🧹 Delete EKS smoke cluster"))

	minutes := 15 // Final job-level headroom in addition to every bounded step.

	for _, step := range job.Steps {
		require.Positive(t, step.TimeoutMinutes, "unbounded step: %s", step.Name)
		minutes += step.TimeoutMinutes
	}

	assert.GreaterOrEqual(t, job.TimeoutMinutes, minutes)
}

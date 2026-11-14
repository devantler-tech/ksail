package ciharness_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

//nolint:funlen // One structural test binds all stages of the workflow handoff and cleanup order.
func TestEKSOwningResourceCaptureIsOptInCumulativeAndCannotSkipCleanup(t *testing.T) {
	t.Parallel()

	var trigger struct {
		On map[string]struct {
			Inputs map[string]struct {
				Type    string `yaml:"type"`
				Default any    `yaml:"default"`
			} `yaml:"inputs"`
		} `yaml:"on"`
	}
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-eks.yaml"), &trigger),
	)
	input := trigger.On["workflow_dispatch"].Inputs["capture_resource_ownership"]
	require.Equal(t, "boolean", input.Type)
	require.Equal(t, false, input.Default)

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	steps := workflow.Jobs["smoke-test"].Steps
	baseline := findHarnessStep(t, steps, "🔎 Capture EKS regional baseline")
	require.Equal(t, 5, baseline.TimeoutMinutes)
	require.Contains(t, baseline.If, "inputs.capture_resource_ownership")
	require.Contains(t, baseline.Run, "mkdir -m 700")
	require.Contains(t, baseline.Run, "--baseline-file")
	require.Contains(t, baseline.Run, "--run-attempt \"$GITHUB_RUN_ATTEMPT\"")

	scaling := findHarnessStep(t, steps, "🧪 ksail cluster update scales EKS nodes")
	scaleUpIndex := strings.Index(scaling.Run, "wait_for_capacity \"$KSAIL_EKS_SMOKE_PEAK_NODES\"")
	capture := strings.Index(scaling.Run, "--ownership-file \"$KSAIL_EKS_OWNERSHIP_FILE\"")
	down := strings.LastIndex(scaling.Run, "set_nodegroup_capacity 1 1 1")

	require.NotEqual(t, -1, scaleUpIndex)
	require.NotEqual(t, -1, capture)
	require.NotEqual(t, -1, down)
	assert.Less(t, scaleUpIndex, capture, "capture must observe the scaled-up identities")
	assert.Less(t, capture, down, "capture must precede scale-down detachment")
	require.Contains(t, scaling.Run, "timeout --kill-after=10s 5m")

	finalCapture := findHarnessStep(t, steps, "🔎 Preserve EKS ownership before cleanup")
	require.Equal(t, 5, finalCapture.TimeoutMinutes)
	require.Contains(t, finalCapture.If, "always()")
	require.Contains(t, finalCapture.If, "inputs.capture_resource_ownership")
	require.Contains(t, finalCapture.Run, "kubectl get services --all-namespaces -o json")
	require.Contains(t, finalCapture.Run, "kubectl get persistentvolumes -o json")
	require.Contains(t, finalCapture.Run, "--load-balancer-host")
	require.Contains(t, finalCapture.Run, "--volume-id")
	require.Contains(t, finalCapture.Run, "--workload-inventory-complete")

	finalIndex := harnessStepIndex(t, steps, "🔎 Preserve EKS ownership before cleanup")
	deleteIndex := harnessStepIndex(t, steps, "🧹 Delete EKS smoke cluster")
	verifyIndex := harnessStepIndex(t, steps, "🔎 Verify EKS cleanup from private ownership")
	eraseIndex := harnessStepIndex(t, steps, "🧽 Remove private EKS evidence")
	assert.Less(t, finalIndex, deleteIndex)
	assert.Less(t, deleteIndex, verifyIndex)
	assert.Less(t, verifyIndex, eraseIndex)
	assert.Less(t, deleteIndex, eraseIndex)
	deleteStep := steps[deleteIndex]
	require.Contains(
		t,
		deleteStep.If,
		"always()",
		"capture failure must still reach both deletion paths",
	)

	verify := steps[verifyIndex]
	require.Equal(t, 5, verify.TimeoutMinutes)
	require.Contains(t, verify.If, "always()")
	require.Contains(t, verify.If, "inputs.capture_resource_ownership")
	require.Contains(t, verify.Run, "--verify-ownership-file \"$KSAIL_EKS_OWNERSHIP_FILE\"")
	require.Contains(t, verify.Run, "--run-attempt \"$GITHUB_RUN_ATTEMPT\"")
	require.NotContains(t, verify.Run, "aws ")

	erase := steps[eraseIndex]
	require.Contains(t, erase.If, "always()")
	require.Contains(
		t,
		erase.Run,
		"rm -f -- \"$KSAIL_EKS_BASELINE_FILE\" \"$KSAIL_EKS_OWNERSHIP_FILE\"",
	)
}

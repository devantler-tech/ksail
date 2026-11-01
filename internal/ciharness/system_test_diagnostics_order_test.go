package ciharness_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	systemTestActionPath      = ".github/actions/ksail-system-test/action.yaml"
	saveDiagnosticsStepName   = "📋 Save system test diagnostics"
	uploadDiagnosticsStepName = "📤 Upload system test diagnostics"
)

// stepsAllowedAfterDiagnostics returns the only steps that may follow the diagnostics capture
// in the system-test action. Each one tears down or uploads; none of them tests anything.
func stepsAllowedAfterDiagnostics() []string {
	return []string{
		uploadDiagnosticsStepName,
		"🧪 Cleanup KSail System Test",
		"📝 Finalize test timeline",
		"📤 Upload system test logs",
	}
}

var errDiagnosticsStepMissing = errors.New("the system-test action has no diagnostics step")

// TestSystemTestDiagnosticsCoverEveryTestPhase guards ksail#7700. The diagnostics steps run on
// failure() and capture the cluster before cleanup, so a test phase placed after them can fail
// without leaving any diagnostics behind: the Hetzner autoscaler inventory trial did.
func TestSystemTestDiagnosticsCoverEveryTestPhase(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, systemTestActionPath)

	late, err := stepsAfterDiagnostics(action.Runs.Steps)
	require.NoError(t, err)
	assert.Empty(t, late, "these steps run after the diagnostics are captured; move them before %q",
		saveDiagnosticsStepName)

	save := findHarnessStep(t, action.Runs.Steps, saveDiagnosticsStepName)
	assert.Equal(t, "failure()", save.If)

	upload := findHarnessStep(t, action.Runs.Steps, uploadDiagnosticsStepName)
	assert.Equal(t, "failure() && inputs.upload-artifacts == 'true'", upload.If)

	trial := harnessStepIndex(
		t,
		action.Runs.Steps,
		"🧪 Hetzner autoscaler inventory — unchanged update and retry",
	)
	assert.Less(t, trial, harnessStepIndex(t, action.Runs.Steps, saveDiagnosticsStepName))
}

// TestStepsAfterDiagnostics checks the order rule against fixtures, so the guard above cannot
// pass because it stopped looking.
func TestStepsAfterDiagnostics(t *testing.T) {
	t.Parallel()

	steps := func(names ...string) []harnessStep {
		result := make([]harnessStep, 0, len(names))
		for _, name := range names {
			result = append(result, harnessStep{Name: name})
		}

		return result
	}

	testCases := []struct {
		name    string
		steps   []harnessStep
		want    []string
		wantErr error
	}{
		{
			name: "only teardown and uploads follow the diagnostics",
			steps: steps(
				"🧪 ksail cluster create",
				saveDiagnosticsStepName,
				uploadDiagnosticsStepName,
				"🧪 Cleanup KSail System Test",
			),
		},
		{
			name: "a test phase follows the diagnostics",
			steps: steps(
				saveDiagnosticsStepName,
				uploadDiagnosticsStepName,
				"🧪 Hetzner autoscaler inventory — unchanged update and retry",
				"🧪 Cleanup KSail System Test",
			),
			want: []string{"🧪 Hetzner autoscaler inventory — unchanged update and retry"},
		},
		{
			// An allow list, so a new step of any name after the diagnostics is reported.
			name:  "an unknown step follows the diagnostics",
			steps: steps(saveDiagnosticsStepName, "📦 Setup something new"),
			want:  []string{"📦 Setup something new"},
		},
		{
			name:    "no diagnostics step",
			steps:   steps("🧪 ksail cluster create", "🧪 Cleanup KSail System Test"),
			wantErr: errDiagnosticsStepMissing,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := stepsAfterDiagnostics(testCase.steps)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// stepsAfterDiagnostics returns the steps after the diagnostics capture that are not in
// stepsAllowedAfterDiagnostics.
func stepsAfterDiagnostics(steps []harnessStep) ([]string, error) {
	index := slices.IndexFunc(steps, func(step harnessStep) bool {
		return step.Name == saveDiagnosticsStepName
	})
	if index < 0 {
		return nil, fmt.Errorf("%w: %q", errDiagnosticsStepMissing, saveDiagnosticsStepName)
	}

	allowed := stepsAllowedAfterDiagnostics()

	var late []string

	for _, step := range steps[index+1:] {
		if !slices.Contains(allowed, step.Name) {
			late = append(late, step.Name)
		}
	}

	return late, nil
}

package ciharness_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEKSSmokeInstallsTheBuildJobBinary keeps the paid EKS trial from compiling
// KSail. A missed cache once made the smoke job rebuild into its 10-minute step
// budget and fail before any cluster existed (ksail#8185).
func TestEKSSmokeInstallsTheBuildJobBinary(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	consumer := workflow.Jobs["smoke-test"]
	assert.Contains(t, consumer.Needs, "build-artifact")

	restore := findHarnessStep(t, consumer.Steps, "📥 Restore KSail binary")
	assert.Equal(t,
		"devantler-tech/ksail/.github/actions/restore-ksail-binary@"+
			"fc38423661fd6eaceb03ec49cd0634108c4ac18f", restore.Uses)
	assert.Equal(
		t,
		"${{ needs.build-artifact.outputs.artifact-id }}",
		stringValue(restore.With["artifact-id"]),
	)
	assert.Equal(
		t,
		"${{ needs.build-artifact.outputs.binary-sha256 }}",
		stringValue(restore.With["binary-sha256"]),
	)
	assert.Empty(t, restore.If, "the trial always requires the producer's exact binary")
	assert.False(t, restore.ContinueOnError, "a missing handoff must stop the trial")
	assert.Positive(t, restore.TimeoutMinutes)
	assert.LessOrEqual(t, restore.TimeoutMinutes, 5, "a handoff takes seconds, not a build")
	assert.Less(
		t,
		harnessStepIndex(t, consumer.Steps, restore.Name),
		harnessStepIndex(t, consumer.Steps, "🔧 Initialize EKS project"),
		"the binary must be installed before its first use",
	)

	for _, step := range consumer.Steps {
		assert.NotContains(
			t,
			step.Uses,
			"cache-ksail-binary",
			"a missed cache must not trigger a second compilation",
		)
		// Small helpers may still `go run`; only a KSail build is ruled out here.
		assert.NotRegexp(
			t,
			`\bgo (build|install)\b|go tool bundler`,
			step.Run,
			"the smoke job must not compile KSail",
		)
	}
}

// TestEKSCleanupVerificationNamesARunThatNeverCreated checks the always-run
// ownership verifier. A run that stopped before cluster create created nothing
// and recorded no ownership, so it says that instead of reporting unknown data.
// Every other state still reaches the fail-closed verifier.
func TestEKSCleanupVerificationNamesARunThatNeverCreated(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	verify := findHarnessStep(
		t,
		workflow.Jobs["smoke-test"].Steps,
		"🔎 Verify EKS cleanup from private ownership",
	)
	assert.Equal(t, "${{ steps.create.outcome }}", verify.Env["EKS_CREATE_OUTCOME"])
	assert.Equal(t, "${{ steps.create.outputs.attempted }}", verify.Env["EKS_CREATE_ATTEMPTED"])

	for _, testCase := range []struct {
		name, outcome, attempted string
		ownershipRecorded        bool
		wantVerifier             bool
	}{
		{name: "create-skipped", outcome: "skipped"},
		{name: "create-failed", outcome: "failure", attempted: "true", wantVerifier: true},
		{name: "create-cancelled", outcome: "cancelled", attempted: "true", wantVerifier: true},
		{name: "create-succeeded", outcome: "success", attempted: "true", wantVerifier: true},
		{name: "create-unevaluated", wantVerifier: true},
		{name: "skipped-but-attempted", outcome: "skipped", attempted: "true", wantVerifier: true},
		{
			name: "skipped-with-ownership", outcome: "skipped",
			ownershipRecorded: true, wantVerifier: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, verifierRan, err := runEKSCleanupVerification(
				t, verify, testCase.outcome, testCase.attempted, testCase.ownershipRecorded,
			)
			if testCase.wantVerifier {
				require.Error(t, err, output)
				assert.True(t, verifierRan, "the fail-closed verifier must run")
				assert.NotContains(t, output, "cleanup-verdict=not-started")

				return
			}

			require.NoError(t, err, output)
			assert.False(t, verifierRan, "a run that never created has nothing to verify")
			assert.Contains(t, output, "cleanup-verdict=not-started reason=create-not-attempted")
		})
	}
}

func runEKSCleanupVerification(
	t *testing.T,
	verify harnessStep,
	outcome, attempted string,
	ownershipRecorded bool,
) (string, bool, error) {
	t.Helper()

	workspace := t.TempDir()
	scripts := filepath.Join(workspace, ".github", "scripts")
	require.NoError(t, os.MkdirAll(scripts, 0o750))

	marker := filepath.Join(workspace, "verifier-ran")
	writeExecutableStub(t, filepath.Join(scripts, "report-eks-account-capacity.sh"),
		"#!/bin/sh\n: > \"$VERIFIER_MARKER\"\necho 'cleanup-verdict=unknown'\nexit 1\n")

	ownership := filepath.Join(workspace, "ownership.json")
	if ownershipRecorded {
		require.NoError(t, os.WriteFile(ownership, []byte("{}"), 0o600))
	}

	output, err := runBinaryArtifactStep(t, verify,
		"GITHUB_WORKSPACE="+workspace,
		"VERIFIER_MARKER="+marker,
		"KSAIL_EKS_OWNERSHIP_FILE="+ownership,
		"EKS_CREATE_OUTCOME="+outcome,
		"EKS_CREATE_ATTEMPTED="+attempted,
		"AWS_REGION=eu-central-1",
		"KSAIL_EKS_CLUSTER_NAME=st-eks-1-1",
		"GITHUB_RUN_ID=1",
		"GITHUB_RUN_ATTEMPT=1",
		"GITHUB_SHA=0000000000000000000000000000000000000000",
	)

	_, statErr := os.Stat(marker)

	return output, statErr == nil, err
}

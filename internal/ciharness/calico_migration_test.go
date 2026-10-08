package ciharness_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalicoMigrationUsesImmutableBaselineOnlyForCreate(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	producer := workflow.Jobs["build-artifact"]
	checkout := findHarnessStep(t, producer.Steps, "📄 Checkout Calico 3.32.2 baseline")
	assert.Equal(t, "66ba54d8de006cac38575c3697c863f453fc0f1b", stringValue(checkout.With["ref"]))
	assert.Equal(t, false, checkout.With["persist-credentials"])
	assert.Equal(t, "calico-legacy-source", stringValue(checkout.With["path"]))
	assert.Contains(t, producer.Outputs["calico-baseline-artifact-id"], "calico-baseline-artifact.outputs.artifact-id")
	assert.Contains(t, producer.Outputs["calico-baseline-sha256"], "calico-baseline-build.outputs.binary-sha256")

	consumer := workflow.Jobs["system-test-docker"]
	download := findHarnessStep(t, consumer.Steps, "📥 Download Calico migration baseline")
	assert.Equal(t, "${{ matrix.calico-migration == 'true' }}", download.If)
	assert.Equal(t, "${{ needs.build-artifact.outputs.calico-baseline-artifact-id }}", stringValue(download.With["artifact-ids"]))
	assert.Equal(t, "${{ runner.temp }}/calico-legacy-download", stringValue(download.With["path"]))
	assert.NotContains(t, download.With, "name")

	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	prepare := findHarnessStep(t, action.Runs.Steps, "🔍 Prepare Calico migration creation binary")
	create := findHarnessStep(t, action.Runs.Steps, "🧪 ksail cluster create")
	migrate := findHarnessStep(t, action.Runs.Steps, "🧪 Migrate Calico with unchanged configuration")
	assert.Equal(t, "${{ steps.calico-create-path.outputs.path }}", create.Env["PATH"])
	assert.Contains(t, prepare.Env["BASELINE_SHA256"], "inputs.calico-migration-sha256")
	assert.NotContains(t, prepare.Run, "GITHUB_PATH", "the baseline must not select the CLI used after creation")
	assert.NotContains(t, migrate.Env, "PATH", "updates must use the candidate CLI")
	assert.Less(t, harnessStepIndex(t, action.Runs.Steps, prepare.Name), harnessStepIndex(t, action.Runs.Steps, create.Name))
	assert.Less(t, harnessStepIndex(t, action.Runs.Steps, create.Name), harnessStepIndex(t, action.Runs.Steps, migrate.Name))
	assert.Less(t, harnessStepIndex(t, action.Runs.Steps, migrate.Name), harnessStepIndex(t, action.Runs.Steps, "🧪 ksail cluster info"))
	assert.Contains(t, migrate.Run, "calico-migration.sh")
}

func TestCalicoMigrationRejectsInvalidProof(t *testing.T) {
	t.Parallel()

	output, err := runBinaryArtifactStep(t, harnessStep{
		Run: "bash ../../.github/actions/ksail-system-test/calico-migration.test.sh",
	})
	require.NoError(t, err, output)
	assert.Contains(t, output, "PASS: migration verifies the legacy release")
}

func TestCalicoMigrationKeepsPrivateTargetWithGHCRCredentials(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	resolve := findHarnessStep(t, action.Runs.Steps, "🔧 Resolve GHCR credentials in args")
	directory := t.TempDir()
	outputs := filepath.Join(directory, "outputs")
	output, err := runBinaryArtifactStep(t, resolve,
		"CALICO_BASELINE=/fixture/ksail",
		"RUNNER_TEMP="+directory,
		"GITHUB_OUTPUT="+outputs,
		"GITHUB_ENV="+filepath.Join(directory, "environment"),
		"ARGS=--cni Calico --local-registry ghcr.io/example/manifests",
		"GHCR_USER=fixture", "GHCR_TOKEN=fixture", "ARTIFACT_TAG=fixture",
		"SCHEMATIC_TRIAL=false", "SERVER_TYPE=", "UPGRADE_FROM=", "UPGRADE_TO=")
	require.NoError(t, err, output)

	contents, err := os.ReadFile(outputs)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "--kubeconfig "+directory+"/calico-migration.kubeconfig")
	assert.Contains(t, string(contents), "--local-registry fixture:fixture@ghcr.io/example/manifests:fixture")
}

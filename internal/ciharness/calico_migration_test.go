package ciharness_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestCalicoMigrationK3sKeepsEmbeddedLoadBalancerBaseline(t *testing.T) {
	t.Parallel()

	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []map[string]any `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(readRepoFile(t, ".github/workflows/ci.yaml"), &workflow))

	var legs int

	for _, entry := range workflow.Jobs["system-test-docker"].Strategy.Matrix.Include {
		if entry["calico-migration"] != "true" {
			continue
		}

		legs++

		args := stringValue(entry["args"])
		if entry["distribution"] == "K3s" {
			require.Contains(
				t,
				args,
				"--load-balancer Enabled",
				"the immutable legacy K3s CLI leaves its embedded load balancer enabled",
			)
		} else {
			require.Contains(t, args, "--load-balancer Disabled")
		}
	}

	require.Equal(t, 3, legs)
}

func TestCalicoMigrationPublishesImmutableBaseline(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	producer := workflow.Jobs["build-calico-baseline"]
	checkout := findHarnessStep(t, producer.Steps, "📄 Checkout Calico 3.32.2 baseline")
	assert.Equal(t, "66ba54d8de006cac38575c3697c863f453fc0f1b", stringValue(checkout.With["ref"]))
	assert.Equal(t, false, checkout.With["persist-credentials"])
	assert.Equal(t, "calico-legacy-source", stringValue(checkout.With["path"]))
	assert.Contains(
		t,
		producer.Outputs["calico-baseline-artifact-id"],
		"calico-baseline-artifact.outputs.artifact-id",
	)
	assert.Contains(
		t,
		producer.Outputs["calico-baseline-sha256"],
		"calico-baseline-build.outputs.binary-sha256",
	)
}

func TestCalicoMigrationUsesImmutableBaselineOnlyForCreate(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	consumer := workflow.Jobs["system-test-docker"]
	download := findHarnessStep(t, consumer.Steps, "📥 Download Calico migration baseline")
	assert.Equal(t, "${{ matrix.calico-migration == 'true' }}", download.If)
	assert.Equal(
		t,
		"${{ needs.build-calico-baseline.outputs.calico-baseline-artifact-id }}",
		stringValue(download.With["artifact-ids"]),
	)
	assert.Equal(t, "${{ runner.temp }}/calico-legacy-download", stringValue(download.With["path"]))
	assert.NotContains(t, download.With, "name")

	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	prepare := findHarnessStep(t, action.Runs.Steps, "🔍 Prepare Calico migration creation binary")
	create := findHarnessStep(t, action.Runs.Steps, "🧪 ksail cluster create")
	migrate := findHarnessStep(
		t,
		action.Runs.Steps,
		"🧪 Migrate Calico with unchanged configuration",
	)
	assert.Equal(t, "${{ steps.calico-create-path.outputs.path }}", create.Env["PATH"])
	assert.Contains(t, prepare.Env["BASELINE_SHA256"], "inputs.calico-migration-sha256")
	assert.NotContains(
		t,
		prepare.Run,
		"GITHUB_PATH",
		"the baseline must not select the CLI used after creation",
	)
	assert.NotContains(t, migrate.Env, "PATH", "updates must use the candidate CLI")
	assert.Less(
		t,
		harnessStepIndex(t, action.Runs.Steps, prepare.Name),
		harnessStepIndex(t, action.Runs.Steps, create.Name),
	)
	assert.Less(
		t,
		harnessStepIndex(t, action.Runs.Steps, create.Name),
		harnessStepIndex(t, action.Runs.Steps, migrate.Name),
	)
	assert.Less(
		t,
		harnessStepIndex(t, action.Runs.Steps, migrate.Name),
		harnessStepIndex(t, action.Runs.Steps, "🧪 ksail cluster info"),
	)
	assert.Contains(t, migrate.Run, "calico-migration.sh")
}

// Candidate generation and cache warming must not wait for a second, independent
// compiler invocation. Migration legs still require its verified artifact.
func TestCalicoBaselineBuildDoesNotDelayCandidateConsumers(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	baseline, found := workflow.Jobs["build-calico-baseline"]
	require.True(t, found, "the migration baseline needs an independent producer")
	assert.Equal(t, []string{"changes"}, baseline.Needs)
	assert.False(t, baseline.ContinueOnError)
	assert.Equal(t, "read", baseline.Permissions["contents"])
	assert.Len(t, baseline.Permissions, 1)

	for _, name := range []string{"build-artifact", "generate", "warm-helm-cache", "warm-mirror-cache"} {
		job := workflow.Jobs[name]
		assert.NotContains(t, job.Needs, "build-calico-baseline", name)

		for _, step := range job.Steps {
			assert.NotContains(t, step.Run, "calico-legacy-source", name)
		}
	}

	consumer := workflow.Jobs["system-test-docker"]
	assert.Contains(t, consumer.Needs, "build-calico-baseline")
	assert.Contains(t, consumer.If, "needs.build-calico-baseline.result == 'success'")

	gate := workflow.Jobs["require-checks-in-pr"]
	assert.Contains(t, gate.Needs, "build-calico-baseline")
	assert.Contains(t, gate.Steps[0].With["job-results"], "needs.build-calico-baseline.result")
}

func TestCalicoBaselineSetupPinsReviewedAction(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	step := findHarnessStep(t, workflow.Jobs["build-calico-baseline"].Steps, "🧹 Free disk space")
	assert.Equal(
		t,
		"devantler-tech/ksail/.github/actions/free-disk-space@6c48c4c282c27545d059733b011d98f0b49585ff",
		step.Uses,
	)

	// The immutable protected-main action has exactly these candidate bytes.
	// Future action changes need a reviewed pin refresh rather than stale setup.
	for path, digest := range map[string]string{
		"action.yaml":        "bb40116a5901c77432b0d330b05e56228d9546dab26f98a31f05a0e87ffab96c",
		"free-disk-space.sh": "2dd12fcf3779137ca1cb5f21194947f6a10d3f94f8fb438a1a29ec9021fbdc30",
	} {
		contents := readRepoFile(t, ".github/actions/free-disk-space/"+path)
		assert.Equal(t, digest, fmt.Sprintf("%x", sha256.Sum256(contents)), path)
	}
}

func TestSelectedCalicoBaselineCannotSilentlySkip(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	gate := workflow.Jobs["require-checks-in-pr"]
	step := findHarnessStep(t, gate.Steps, "🔍 Require the selected Calico baseline")
	assert.Contains(t, step.If, "github.event_name == 'pull_request'")
	assert.Contains(t, step.If, "needs.changes.outputs.system-test == 'true'")
	assert.Contains(t, step.If, "inputs.run_system_tests == true")
	assert.Contains(t, step.If, "!inputs.run_local_recovery_trial")
	assert.Equal(t, "${{ needs.build-calico-baseline.result }}", step.Env["BASELINE_RESULT"])
	assert.False(t, step.ContinueOnError)

	for _, result := range []string{"success", "failure", "cancelled", "skipped", ""} {
		t.Run(result, func(t *testing.T) {
			t.Parallel()

			_, err := runBinaryArtifactStep(t, step, "BASELINE_RESULT="+result)
			if result == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err, "a selected migration needs a successful producer")
			}
		})
	}
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

	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	contents, err := root.ReadFile("outputs")
	require.NoError(t, err)
	assert.Contains(t, string(contents), "--kubeconfig "+directory+"/calico-migration.kubeconfig")
	assert.Contains(
		t,
		string(contents),
		"--local-registry fixture:fixture@ghcr.io/example/manifests:fixture",
	)
}

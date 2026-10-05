package ciharness_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfig "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfig "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/configdiff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

//nolint:tagliatelle // GitHub Actions defines this external key in snake_case.
type hetznerWorkflow struct {
	On struct {
		WorkflowDispatch struct {
			Inputs map[string]struct {
				Type    string   `yaml:"type"`
				Options []string `yaml:"options"`
				Default any      `yaml:"default"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
	Jobs map[string]struct {
		If       string `yaml:"if"`
		Strategy struct {
			Matrix map[string]any `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []harnessStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestHetznerWorkflowAllowsManualDirectProviderSmoke(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-hetzner.yaml")

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	distribution, found := workflow.On.WorkflowDispatch.Inputs["distribution"]
	require.True(t, found, "workflow dispatch must expose a distribution selector")
	assert.Equal(t, "choice", distribution.Type)
	assert.Equal(t, "Talos", distribution.Default)
	assert.ElementsMatch(t, []string{"Talos", "K3s", "Vanilla"}, distribution.Options)

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")

	include, ok := systemTest.Strategy.Matrix["include"].(string)
	require.True(t, ok, "Hetzner matrix include must remain an expression string")
	assert.Contains(t, include, "inputs.distribution")
	assert.Contains(t, include, "inputs.distribution != 'Talos'")

	args := findHarnessStep(t, systemTest.Steps, "🔧 Build args string")
	assert.Contains(t, args.Run, `matrix.smoke != true`)
}

func TestHetznerManualSchematicRolloutIsOptInAndKeepsCleanup(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(
		readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow,
	))

	selector, found := workflow.On.WorkflowDispatch.Inputs["test_schematic_rollout"]
	require.True(t, found, "manual dispatch must expose the schematic rollout trial")
	assert.Equal(t, "boolean", selector.Type)
	assert.Equal(t, false, selector.Default)

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")
	run := findHarnessStep(t, systemTest.Steps, "🧪 Run KSail System Test")
	selection := findHarnessStep(t, systemTest.Steps, "✅ Validate Talos schematic trial selection")
	assert.Contains(t, selection.If, "inputs.test_schematic_rollout")
	assert.Contains(t, selection.Run, "Talos")
	assert.Contains(t, selection.Run, "INIT")
	assert.Contains(t, selection.Run, "RESIZE_TYPE")
	assert.Less(t,
		harnessStepIndex(t, systemTest.Steps, selection.Name),
		harnessStepIndex(t, systemTest.Steps, run.Name),
	)
	assert.Less(t,
		harnessStepIndex(t, systemTest.Steps, selection.Name),
		harnessStepIndex(t, systemTest.Steps, "🧪 Create Hetzner Smoke Cluster"),
		"invalid trial selection must fail before any provider resources are created",
	)
	assert.Equal(t,
		"${{ github.event_name == 'workflow_dispatch' && inputs.test_schematic_rollout || false }}",
		run.With["test-talos-schematic-rollout"],
	)
	assertSchematicRolloutActionSelection(t)

	cleanup, found := workflow.Jobs["cleanup"]
	require.True(t, found, "workflow-level fallback cleanup is missing")
	assert.Contains(t, cleanup.If, "always()")
}

func assertSchematicRolloutActionSelection(t *testing.T) {
	t.Helper()

	var action compositeAction
	require.NoError(t, yaml.Unmarshal(
		readRepoFile(t, ".github/actions/ksail-system-test/action.yaml"), &action,
	))
	rollout := findHarnessStep(
		t,
		action.Runs.Steps,
		"🧪 ksail cluster update — same-version Talos schematic",
	)
	assert.Contains(t, rollout.If, "inputs.provider == 'Hetzner'")
	assert.Contains(t, rollout.If, "inputs.distribution == 'Talos'")
	assert.Contains(t, rollout.If, "inputs.test-talos-schematic-rollout == 'true'")
	assert.Contains(t, rollout.Run, "Would reconcile distribution image")
	assert.Contains(t, rollout.Run, "No changes detected")
	assert.Equal(t, "${{ steps.k8s-version-pin.outputs.flag }}", rollout.Env["K8S_VERSION_FLAG"])
	assert.Greater(t,
		harnessStepIndex(t, action.Runs.Steps, rollout.Name),
		harnessStepIndex(t, action.Runs.Steps, "🧪 ksail cluster update"),
	)
	assert.Less(t,
		harnessStepIndex(t, action.Runs.Steps, rollout.Name),
		harnessStepIndex(t, action.Runs.Steps, "🧪 ksail cluster stop"),
	)
}

func TestHetznerFallbackCleanupSurvivesCancellation(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(
		readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow,
	))
	cleanup, found := workflow.Jobs["cleanup"]
	require.True(t, found, "workflow-level fallback cleanup is missing")
	assert.Equal(t, "always()", cleanup.If)
	require.NotEmpty(t, cleanup.Steps)

	for _, step := range cleanup.Steps {
		t.Run(step.Name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "${{ always() }}", step.If,
				"fallback steps must remain eligible after cancellation")

			if strings.HasPrefix(step.Uses, "actions/checkout@") {
				return
			}

			assert.Equal(t, "$/.github/actions/cleanup-hetzner", step.Uses,
				"self-repository actions must resolve at the workflow's exact commit")
			readRepoFile(t, "./.github/actions/cleanup-hetzner/action.yaml")
			assert.Contains(t, step.With["label-selector"], "${{ github.run_id }}",
				"cleanup must stay scoped to this run's owned resources")
		})
	}
}

type hetznerSchematicScenario struct {
	name        string
	liveVersion string
	wantSuccess bool
	wantNoApply bool
	wantNoPlan  bool
}

type schematicRolloutFixture struct {
	project      string
	fakeBin      string
	logDir       string
	callsFile    string
	dryCountFile string
}

func TestHetznerSchematicRolloutRequiresLiveDriftAndReadback(t *testing.T) {
	t.Parallel()

	var action compositeAction
	require.NoError(t, yaml.Unmarshal(
		readRepoFile(t, ".github/actions/ksail-system-test/action.yaml"), &action,
	))
	rollout := findHarnessStep(
		t,
		action.Runs.Steps,
		"🧪 ksail cluster update — same-version Talos schematic",
	)

	for _, scenario := range []hetznerSchematicScenario{
		{name: "converged", wantSuccess: true},
		{name: "converged-upgraded", liveVersion: "v1.14.2", wantSuccess: true},
		{name: "pin-mismatch", liveVersion: "v1.14.2", wantNoPlan: true, wantNoApply: true},
		{name: "missing-pin", wantNoPlan: true, wantNoApply: true},
		{name: "missing-kubernetes-pin", wantNoPlan: true, wantNoApply: true},
		{name: "invalid-kubernetes-pin", wantNoPlan: true, wantNoApply: true},
		{name: "factory-failed", wantNoPlan: true, wantNoApply: true},
		{name: "factory-mismatch", wantNoPlan: true, wantNoApply: true},
		{name: "pre-missing", wantNoApply: true},
		{name: "pre-config-drift", wantNoApply: true},
		{name: "not-ready"},
		{name: "post-drift"},
		{name: "mixed-version", wantNoPlan: true, wantNoApply: true},
		{name: "missing-version", wantNoPlan: true, wantNoApply: true},
		{name: "non-talos", wantNoPlan: true, wantNoApply: true},
		{name: "mixed-missing-version", wantNoPlan: true, wantNoApply: true},
		{name: "mixed-non-talos", wantNoPlan: true, wantNoApply: true},
		{name: "no-nodes", wantNoPlan: true, wantNoApply: true},
		{name: "nodes-query-failed", wantNoPlan: true, wantNoApply: true},
		{name: "malformed-nodes", wantNoPlan: true, wantNoApply: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			assertHetznerSchematicRolloutScenario(t, rollout.Run, scenario)
		})
	}
}

func assertHetznerSchematicRolloutScenario(
	t *testing.T,
	rollout string,
	scenario hetznerSchematicScenario,
) {
	t.Helper()
	fixture := newSchematicRolloutFixture(t)

	liveVersion := scenario.liveVersion
	if liveVersion == "" {
		liveVersion = "v1.12.4"
	}

	writeSchematicBaseline(t, fixture, scenario, liveVersion)

	var before *talosconfig.Configs
	if scenario.wantSuccess {
		before = loadRenderedSchematicConfigs(t, fixture.project)
	}

	output, err := runSchematicRolloutScenario(t, fixture, rollout, scenario.name, liveVersion)
	if scenario.wantSuccess {
		require.NoErrorf(t, err, "rollout failed:\n%s", output)
	} else {
		require.Errorf(t, err, "rollout accepted missing live evidence:\n%s", output)
	}

	calls, readErr := os.ReadFile(fixture.callsFile)
	require.NoError(t, readErr)
	assertSchematicRolloutCalls(t, string(calls), scenario)

	if scenario.wantSuccess {
		config, readErr := os.ReadFile(filepath.Join(fixture.project, "ksail.yaml"))
		require.NoError(t, readErr)
		assert.Contains(t, string(config), liveVersion)
		assert.Contains(
			t,
			string(config),
			"schematicId: c9078f9419961640c712a8bf2bb9174933dfcf1da383fd8ea2b7dc21493f8bac",
		)
		assert.NotContains(t, string(config), "extensions:")
		assertSchematicRenderUnchanged(t, before, loadRenderedSchematicConfigs(t, fixture.project))
	}
}

func runSchematicRolloutScenario(
	t *testing.T,
	fixture schematicRolloutFixture,
	rollout, scenario, liveVersion string,
) ([]byte, error) {
	t.Helper()

	kubernetesFlag := "--kubernetes-version v1.35.0"

	switch scenario {
	case "missing-kubernetes-pin":
		kubernetesFlag = ""
	case "invalid-kubernetes-pin":
		kubernetesFlag = "--kubernetes-version latest"
	}

	commandContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext( //nolint:gosec // Reviewed action body.
		commandContext,
		"bash",
		"-c",
		rollout,
	)
	command.Dir = fixture.project
	command.Env = append(os.Environ(),
		"PATH="+fixture.fakeBin+":"+os.Getenv("PATH"),
		"ARGS=--name schematic-trial --image-verification cosign",
		"SCENARIO="+scenario,
		"LIVE_TALOS_VERSION="+liveVersion,
		"K8S_VERSION_FLAG="+kubernetesFlag,
		"GITHUB_WORKSPACE="+repositoryRootForSchematicTrial(t),
		"CALLS_FILE="+fixture.callsFile,
		"DRY_COUNT_FILE="+fixture.dryCountFile,
		"KSAIL_SYSTEM_TEST_LOG_DIR="+fixture.logDir,
	)

	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("execute schematic rollout fixture: %w", err)
	}

	return output, nil
}

func loadRenderedSchematicConfigs(t *testing.T, project string) *talosconfig.Configs {
	t.Helper()

	manager := ksailconfig.NewConfigManager(io.Discard, filepath.Join(project, "ksail.yaml"))
	_, err := manager.Load(configmanager.LoadOptions{Silent: true, SkipValidation: true})
	require.NoError(t, err)
	require.NotNil(t, manager.DistributionConfig.Talos)

	return manager.DistributionConfig.Talos
}

func assertSchematicRenderUnchanged(t *testing.T, before, after *talosconfig.Configs) {
	t.Helper()

	secrets, err := before.ExtractSecrets()
	require.NoError(t, err)
	after, err = after.WithSecrets(secrets)
	require.NoError(t, err)

	for _, role := range []string{"control-plane", "worker"} {
		oldConfig, newConfig := before.ControlPlane(), after.ControlPlane()
		if role == "worker" {
			oldConfig, newConfig = before.Worker(), after.Worker()
		}

		diff, diffErr := configdiff.DiffConfigs(
			oldConfig.RedactSecrets("<redacted>"),
			newConfig.RedactSecrets("<redacted>"),
		)
		require.NoError(t, diffErr)
		assert.Empty(t, diff, "%s renderer changed during an image-only trial", role)
	}
}

func assertSchematicRolloutCalls(t *testing.T, calls string, scenario hetznerSchematicScenario) {
	t.Helper()
	assert.Contains(t, calls, "workload get nodes -o json")

	if scenario.wantNoPlan {
		assert.NotContains(t, calls, "cluster update --dry-run")
	} else {
		assert.Contains(t, calls, "cluster update --dry-run")
		assert.Contains(t, calls, "--name schematic-trial")
	}

	assert.NotContains(t, calls, "--image-verification")

	for line := range strings.SplitSeq(calls, "\n") {
		if strings.HasPrefix(line, "cluster update ") {
			assert.Contains(t, line, "--kubernetes-version v1.35.0")
		}
	}

	if scenario.wantNoApply {
		assert.NotContains(t, calls, "cluster update --force")
	} else {
		assert.Contains(t, calls, "cluster update --force")
	}
}

func writeSchematicBaseline(
	t *testing.T,
	fixture schematicRolloutFixture,
	scenario hetznerSchematicScenario,
	liveVersion string,
) {
	t.Helper()

	if scenario.name == "missing-pin" {
		return
	}

	pinnedVersion := liveVersion
	if scenario.name == "pin-mismatch" {
		pinnedVersion = "v1.12.4"
	}

	config := "spec:\n  cluster:\n    distribution: Talos\n    provider: Hetzner\n" +
		"    distributionConfig: " + filepath.Join(fixture.project, "talos") + "\n" +
		"    talos:\n      version: " + pinnedVersion + "\n"
	require.NoError(
		t,
		os.WriteFile(filepath.Join(fixture.project, "ksail.yaml"), []byte(config), 0o600),
	)
}

func repositoryRootForSchematicTrial(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	return root
}

func newSchematicRolloutFixture(t *testing.T) schematicRolloutFixture {
	t.Helper()
	fixture := schematicRolloutFixture{
		project:      t.TempDir(),
		fakeBin:      t.TempDir(),
		logDir:       t.TempDir(),
		callsFile:    filepath.Join(t.TempDir(), "calls"),
		dryCountFile: filepath.Join(t.TempDir(), "dry-count"),
	}
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.project, "talos"), 0o700))
	defaultsDir := filepath.Join(fixture.project, "pkg", "apis", "cluster", "v1alpha1")
	require.NoError(t, os.MkdirAll(defaultsDir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(defaultsDir, "defaults.go"),
		[]byte("package v1alpha1\nconst (\n\tDefaultHetznerTalosVersion = \"v1.12.4\"\n)\n"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(fixture.project, "ksail.yaml"),
		[]byte("spec:\n  cluster:\n    distribution: Talos\n    provider: Hetzner\n"),
		0o600,
	))
	writeSchematicFakeKSail(t, filepath.Join(fixture.fakeBin, "ksail"))
	writeExecutable(t, filepath.Join(fixture.fakeBin, "curl"), `#!/usr/bin/env bash
echo "curl $*" >> "$CALLS_FILE"
if [[ "$SCENARIO" == factory-failed ]]; then exit 22; fi
if [[ "$SCENARIO" == factory-mismatch ]]; then echo 'customization: {}'; exit 0; fi
cat "$GITHUB_WORKSPACE/.github/fixtures/talos-schematic-trial.yaml"
`)
	writeExecutable(t, filepath.Join(fixture.fakeBin, "sleep"), "#!/usr/bin/env bash\nexit 0\n")

	return fixture
}

func writeSchematicFakeKSail(t *testing.T, path string) {
	t.Helper()
	writeExecutable(
		t,
		path,
		string(readRepoFile(t, "internal/ciharness/testdata/schematic_fake_ksail.sh")),
	)
}

func TestHetznerWorkflowSmokesK3sAndVanilla(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-hetzner.yaml")

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")
	assertHetznerSmokeMatrix(t, systemTest.Strategy.Matrix)
	assertHetznerSmokeSteps(t, systemTest.Steps)

	fallback, found := workflow.Jobs["cleanup"]
	require.True(t, found, "workflow-level Hetzner cleanup job is missing")
	assert.Contains(t, fallback.If, "always()")
	assertHetznerFallbackCleanup(t, fallback.Steps)
}

func TestHetznerSmokeReadinessRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-hetzner.yaml")

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")
	reachability := findHarnessStep(t, systemTest.Steps, "🧪 Assert Hetzner Smoke Cluster Reachable")
	assert.Equal(t, 6, reachability.TimeoutMinutes)

	fakeBin := t.TempDir()
	attemptsFile := filepath.Join(t.TempDir(), "attempts")
	writeExecutable(t, filepath.Join(fakeBin, "ksail"), `#!/usr/bin/env bash
set -euo pipefail
attempts=0
if [[ -f "${ATTEMPTS_FILE}" ]]; then
  attempts=$(<"${ATTEMPTS_FILE}")
fi
attempts=$((attempts + 1))
printf '%s' "${attempts}" > "${ATTEMPTS_FILE}"
case "${attempts}" in
  1) exit 1 ;;
  2) printf 'starting\n' ;;
  *) printf 'ok\n' ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "sleep"), "#!/usr/bin/env bash\nexit 0\n")

	// The command is parsed from this repository's workflow, never from user input.
	commandContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext(commandContext, "bash", "-c", reachability.Run) //nolint:gosec

	command.Env = append(
		os.Environ(),
		"ATTEMPTS_FILE="+attemptsFile,
		"PATH="+fakeBin+":"+os.Getenv("PATH"),
	)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "readiness check failed before the API became ready:\n%s", output)

	attempts, err := os.ReadFile(attemptsFile) //nolint:gosec // The test owns this temporary path.
	require.NoError(t, err)
	assert.Equal(t, "3", string(attempts))
}

const vanillaUserDataStep = "🔐 Verify Vanilla Control-Plane User-Data Carries No Signing Material"

func TestHetznerVanillaSmokeVerifiesNodeUserData(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-hetzner.yaml")

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")

	verify := findHarnessStep(t, systemTest.Steps, vanillaUserDataStep)
	assert.Equal(t, "${{ matrix.smoke == true && matrix.distribution == 'Vanilla' }}", verify.If)
	assert.NotZero(t, verify.TimeoutMinutes)
	assert.Contains(
		t,
		verify.Env["PROBE_IMAGE"],
		"@sha256:",
		"the probe image must be pinned by digest",
	)

	// The check must run against a reachable cluster and before it is deleted.
	verifyIndex := harnessStepIndex(t, systemTest.Steps, vanillaUserDataStep)
	assert.Greater(
		t,
		verifyIndex,
		harnessStepIndex(t, systemTest.Steps, "🧪 Assert Hetzner Smoke Cluster Reachable"),
	)
	assert.Less(
		t, verifyIndex, harnessStepIndex(t, systemTest.Steps, "🧹 Delete Hetzner Smoke Cluster"),
	)

	t.Run("runs the verifier through a pod on the control-plane node", func(t *testing.T) {
		t.Parallel()

		calls, err := runVanillaUserDataStep(t, verify.Run, "cp-1")
		require.NoErrorf(t, err, "verification step failed:\n%s", calls)

		assert.Contains(t, calls, `kubectl run userdata-probe --image=probe-image`)
		assert.Contains(t, calls, `"nodeName":"cp-1","hostNetwork":true`)
		assert.Contains(
			t, calls,
			"go run ./pkg/svc/provisioner/cluster/internal/hetznerbase/cmd/verifynodeuserdata "+
				"-- kubectl exec userdata-probe -- sh -c",
		)
	})

	t.Run("fails without a control-plane node instead of skipping", func(t *testing.T) {
		t.Parallel()

		calls, err := runVanillaUserDataStep(t, verify.Run, "")
		require.Error(t, err, "a cluster with no control-plane node must fail the check")
		assert.NotContains(t, calls, "go run", "nothing may be verified without a node")
	})
}

// runVanillaUserDataStep runs the workflow step against fake kubectl and go
// commands that record their arguments, and returns the recorded calls.
func runVanillaUserDataStep(t *testing.T, script, node string) (string, error) {
	t.Helper()

	fakeBin := t.TempDir()
	callsFile := filepath.Join(t.TempDir(), "calls")

	writeExecutable(t, filepath.Join(fakeBin, "kubectl"), `#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >> "${CALLS_FILE}"
if [ "$1" = get ]; then printf '%s' "${CONTROL_PLANE_NODE}"; fi
`)
	writeExecutable(t, filepath.Join(fakeBin, "go"), `#!/usr/bin/env bash
printf 'go %s\n' "$*" >> "${CALLS_FILE}"
`)

	// The command is parsed from this repository's workflow, never from user input.
	commandContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext(commandContext, "bash", "-c", script) //nolint:gosec

	command.Env = append(
		os.Environ(),
		"CALLS_FILE="+callsFile,
		"CONTROL_PLANE_NODE="+node,
		"PROBE_IMAGE=probe-image",
		"PATH="+fakeBin+":"+os.Getenv("PATH"),
	)
	_, runErr := command.CombinedOutput()

	calls, err := os.ReadFile(callsFile) //nolint:gosec // The test owns this temporary path.
	if err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}

	return string(calls), runErr
}

func writeExecutable(t *testing.T, path string, contents string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.NoError(
		t,
		os.Chmod(path, 0o700), //nolint:gosec // Test-owned shell fixture must be executable.
	)
}

func assertHetznerSmokeMatrix(t *testing.T, matrix map[string]any) {
	t.Helper()

	include, ok := matrix["include"].(string)
	require.True(t, ok, "Hetzner matrix include must remain an expression string")

	const fromJSONPrefix = `fromJSON('`

	start := strings.Index(include, fromJSONPrefix)
	require.NotEqual(t, -1, start, "Hetzner matrix must contain a static fromJSON payload")

	payload := include[start+len(fromJSONPrefix):]
	end := strings.Index(payload, `')`)
	require.NotEqual(t, -1, end, "static Hetzner matrix JSON must be terminated")

	type matrixEntry struct {
		Init         bool   `json:"init"`
		Distribution string `json:"distribution"`
		Suffix       string `json:"suffix"`
		Args         string `json:"args"`
		Smoke        bool   `json:"smoke"`
	}

	var entries []matrixEntry
	require.NoError(t, json.Unmarshal([]byte(payload[:end]), &entries))

	var smokeEntries []matrixEntry

	for _, entry := range entries {
		if entry.Smoke {
			smokeEntries = append(smokeEntries, entry)
		}
	}

	assert.Equal(t, []matrixEntry{
		{Distribution: "K3s", Suffix: "k3s-smoke", Smoke: true},
		{Distribution: "Vanilla", Suffix: "vanilla-smoke", Smoke: true},
	}, smokeEntries)
}

func assertHetznerSmokeSteps(t *testing.T, steps []harnessStep) {
	t.Helper()

	fullTest := findHarnessStep(t, steps, "🧪 Run KSail System Test")
	assert.Contains(t, fullTest.If, "matrix.smoke != true")
	assert.Equal(t, "${{ matrix.distribution }}", fullTest.With["distribution"])

	create := findHarnessStep(t, steps, "🧪 Create Hetzner Smoke Cluster")
	assert.Equal(t, "${{ matrix.smoke == true }}", create.If)
	assert.Equal(t, "$/.github/actions/ksail-cluster", create.Uses)
	assert.Equal(t, "${{ matrix.distribution }}", create.With["distribution"])
	assert.Equal(t, "Hetzner", create.With["provider"])
	assert.Equal(t, "false", create.With["init"])
	assert.Equal(t, "false", create.With["install"])
	assert.Equal(t, "false", create.With["cache"])
	assert.Equal(t, "${{ steps.args.outputs.value }}", create.With["args"])

	reachability := findHarnessStep(t, steps, "🧪 Assert Hetzner Smoke Cluster Reachable")
	assert.Equal(t, "${{ matrix.smoke == true }}", reachability.If)
	assert.Contains(t, reachability.Run, `ksail workload get --raw=/readyz`)

	cleanup := findHarnessStep(t, steps, "🧹 Delete Hetzner Smoke Cluster")
	assert.Contains(t, cleanup.If, "always()")
	assert.Contains(t, cleanup.If, "matrix.smoke == true")
	assert.Equal(t, "$/.github/actions/ksail-system-test-cleanup", cleanup.Uses)
	assert.Equal(t, "${{ secrets.HCLOUD_TOKEN }}", cleanup.Env["HCLOUD_TOKEN"])
	assert.Equal(t, "${{ matrix.distribution }}", cleanup.With["distribution"])
	assert.Equal(t, "Hetzner", cleanup.With["provider"])
}

func assertHetznerFallbackCleanup(t *testing.T, steps []harnessStep) {
	t.Helper()

	expected := []struct {
		name     string
		selector string
	}{
		{
			name:     "🧹 Cleanup Hetzner resources (K3s smoke)",
			selector: "ksail.cluster.name=st-hetzner-k3s-smoke-${{ github.run_id }}",
		},
		{
			name:     "🧹 Cleanup Hetzner resources (Vanilla smoke)",
			selector: "ksail.cluster.name=st-hetzner-vanilla-smoke-${{ github.run_id }}",
		},
	}

	for _, want := range expected {
		step := findHarnessStep(t, steps, want.name)
		assert.Equal(t, "$/.github/actions/cleanup-hetzner", step.Uses)
		assert.Equal(t, want.selector, step.With["label-selector"])
	}
}

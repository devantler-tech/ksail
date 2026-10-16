package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestEKSInventoryFailureReachesRequiredAggregate(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	gate := workflow.Jobs["require-checks-in-pr"]
	require.Contains(
		t,
		gate.Needs,
		"eks-smoke-scripts",
		"required checks must wait for the inventory tests",
	)
	require.NotEmpty(t, gate.Steps)
	results := stringValue(gate.Steps[0].With["job-results"])
	boundResult := regexp.MustCompile(
		`(?:^|\s)\$\{\{\s*needs\.eks-smoke-scripts\.result\s*\}\}(?:\s|$)`,
	)
	assert.True(
		t,
		boundResult.MatchString(results),
		"the aggregate must consume the inventory job's real result",
	)
	assert.False(t, workflow.Jobs["eks-smoke-scripts"].ContinueOnError)
}

func TestEKSRegionalBaselineWorkflowIsOptInAndPrivate(t *testing.T) {
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
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/eks-account-capacity.yaml"), &trigger),
	)
	input := trigger.On["workflow_dispatch"].Inputs["capture_resource_baseline"]
	require.Equal(t, "boolean", input.Type)
	require.Equal(t, false, input.Default)
	workflow := readCIWorkflow(t, ".github/workflows/eks-account-capacity.yaml")
	step := findHarnessStep(t, workflow.Jobs["report"].Steps, "🔎 Report regional EKS resources")
	require.Equal(t, "${{ inputs.capture_resource_baseline }}", step.Env["CAPTURE_BASELINE"])

	for _, mode := range []string{"false", "true"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			evaluateRegionalBaselineStep(t, step, mode)
		})
	}
}

func TestEKSOwnershipPermissionProbeIsReadOnlyAndOptIn(t *testing.T) {
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
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/eks-account-capacity.yaml"), &trigger),
	)
	input := trigger.On["workflow_dispatch"].Inputs["evaluate_ownership_reads"]
	require.Equal(t, "boolean", input.Type)
	require.Equal(t, false, input.Default)

	workflow := readCIWorkflow(t, ".github/workflows/eks-account-capacity.yaml")
	step := findHarnessStep(t, workflow.Jobs["report"].Steps, "🔎 Evaluate exact ownership reads")
	require.Equal(t, 5, step.TimeoutMinutes)
	require.Contains(t, step.If, "inputs.evaluate_ownership_reads")
	require.Contains(t, step.Run, "--baseline-file")
	require.Contains(t, step.Run, "--ownership-file")
	require.Contains(t, step.Run, "capture_status")
	require.Contains(t, step.Run, ".coverage.account.readComplete == true")
	require.Contains(t, step.Run, ".coverage.cluster.reason == \"not-found\"")
	require.Contains(t, step.Run, ".coverage.cloudformation.reason == \"not-found\"")
	require.Contains(t, step.Run, "ownership-read-probe cluster-read=")
	require.Contains(t, step.Run, "rm -f --")
	require.NotContains(
		t,
		step.Run,
		"aws ",
		"provider evaluation must exercise the shipped collector",
	)
	require.Equal(t, 40, workflow.Jobs["report"].TimeoutMinutes)
}

func evaluateRegionalBaselineStep(t *testing.T, step harnessStep, mode string) {
	t.Helper()
	dir := t.TempDir()
	scripts := filepath.Join(dir, ".github", "scripts")
	runner := filepath.Join(dir, "runner")

	require.NoError(t, os.MkdirAll(scripts, 0o700))
	require.NoError(t, os.Mkdir(runner, 0o700))
	writeExecutableStub(
		t,
		filepath.Join(scripts, "report-eks-account-capacity.sh"),
		`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" > "$CAPTURED_ARGS"
while (($#)); do
  if [[ "$1" == --baseline-file ]]; then
    umask 077
    printf '{}' > "$2"
  fi
  shift
done
`,
	)
	prepareBaselineStat(t, dir)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(step.Run)

	command.Env = append(
		os.Environ(),
		"GITHUB_WORKSPACE="+dir,
		"RUNNER_TEMP="+runner,
		"CAPTURE_BASELINE="+mode,
		"AWS_REGION=eu-central-1",
		"GITHUB_RUN_ID=123",
		"GITHUB_RUN_ATTEMPT=2",
		"GITHUB_SHA="+strings.Repeat("a", 40),
		"CAPTURED_ARGS="+filepath.Join(
			dir,
			"arguments",
		),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	assertRegionalBaselineResult(t, dir, mode, string(output))
}

func assertRegionalBaselineResult(t *testing.T, dir, mode, output string) {
	t.Helper()

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	arguments, err := root.ReadFile("arguments")
	require.NoError(t, err)

	if mode == "false" {
		require.Equal(t, "--region\neu-central-1\n", string(arguments))
	} else {
		require.Contains(t, string(arguments), "--baseline-file\n")
		require.Contains(
			t,
			string(arguments),
			"--cluster-name\nregional-diagnostic\n--run-id\n123\n--run-attempt\n2\n--source-sha\n"+strings.Repeat(
				"a",
				40,
			),
		)
		require.Contains(
			t,
			output,
			"Private baseline file verified; no manifest is uploaded.",
		)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "runner"))
	require.NoError(t, err)
	require.Empty(t, entries, "runner-local baseline must be removed on exit")
}

func prepareBaselineStat(t *testing.T, dir string) {
	t.Helper()

	if runtime.GOOS != "darwin" {
		return
	}

	writeExecutableStub(t, filepath.Join(dir, "stat"), `#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == -c && "$2" == %a ]] || exit 99
exec /usr/bin/stat -f '%Lp' "$3"
`)
}

func TestEKSInventoryFixtureAndWrapperChangesSelectTheirTests(t *testing.T) {
	t.Parallel()

	patterns := changesFilterPatterns(t, "eks-smoke-scripts")
	for _, path := range []string{
		"internal/maintenance/eksinventory/testdata/resources.json",
		"internal/maintenance/eksinventory/report.go",
		"internal/maintenance/eksinventory/baseline.go",
		"pkg/fsutil/pathutil/canonical.go",
		".github/scripts/report-eks-account-capacity.sh",
	} {
		assert.True(
			t,
			contractFilterMatches(t, patterns, path),
			"change must select inventory tests: %s",
			path,
		)
	}
}

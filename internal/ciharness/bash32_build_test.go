package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const bash32GoProfile = `#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == test ]]
shift
targets=()
while [[ $# -gt 0 && "$1" != -* ]]; do
  targets+=("$1")
  shift
done
if [[ "${KSAIL_PROFILE_NO_TESTS:-false}" == true ]]; then
  exit 0
fi
"$KSAIL_PROFILE_REAL_GO" list -deps -test "${targets[@]}" >> "$KSAIL_PROFILE_DEPS"
if [[ " $* " == *" -list "* ]]; then
  printf '%s\n' 'TestSystemTestTalosUpgradeEmptyArgumentsUnderBash32'
fi
`

func runBash32BuildProfile(t *testing.T, noTests bool) (string, string, error) {
	t.Helper()

	// The intercepted compiler still resolves the real dependency graph of the
	// workflow's targets; it does not execute the Mac-only fixture on Linux.
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
				//nolint:tagliatelle // GitHub Actions defines this external key.
				WorkingDirectory string `yaml:"working-directory"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(readRepoFile(t, ".github/workflows/ci.yaml"), &workflow))
	job, exists := workflow.Jobs["verify-bash-32-workflow-contract"]
	require.True(t, exists)

	var run, directory string

	for _, step := range job.Steps {
		if step.Run != "" {
			run, directory = step.Run, step.WorkingDirectory
		}
	}

	require.NotEmpty(t, run)

	realGo, err := exec.LookPath("go")
	require.NoError(t, err)
	dir := t.TempDir()
	deps := filepath.Join(dir, "dependencies")
	writeExecutableStub(t, filepath.Join(dir, "go"), bash32GoProfile)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(run)
	command.Dir = filepath.Join("..", "..", directory)

	command.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"KSAIL_PROFILE_REAL_GO="+realGo,
		"KSAIL_PROFILE_DEPS="+deps,
	)
	if noTests {
		command.Env = append(command.Env, "KSAIL_PROFILE_NO_TESTS=true")
	}

	output, runErr := command.CombinedOutput()
	root, openErr := os.OpenRoot(dir)
	require.NoError(t, openErr)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	contents, readErr := root.ReadFile("dependencies")
	if !noTests {
		require.NoError(t, readErr)
	}

	return string(contents), string(output), runErr
}

func TestBash32WorkflowKeepsItsBuildSmall(t *testing.T) {
	t.Parallel()
	deps, output, err := runBash32BuildProfile(t, false)
	require.NoError(t, err, output)

	unique := make(map[string]struct{})
	for dependency := range strings.FieldsSeq(deps) {
		unique[dependency] = struct{}{}
	}

	require.NotEmpty(t, unique)
	assert.LessOrEqual(t, len(unique), 300, "a shell fixture must not compile the cluster SDKs")
}

func TestBash32WorkflowRejectsAnUnselectedFixture(t *testing.T) {
	t.Parallel()
	_, output, err := runBash32BuildProfile(t, true)
	require.Error(t, err, "an empty test selection must fail the workflow: %s", output)
}

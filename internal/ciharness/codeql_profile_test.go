package ciharness_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Exercise the actual workflow entrypoint: report contents alone must never
// conceal a failure of the collector process that produced them.
func TestCodeQLProfileBindsActualProcessStatus(t *testing.T) {
	t.Parallel()

	entrypoint := codeQLProfileEntrypoint(
		t, "profile", "Measure every Go project and verify function bodies",
	).Run

	for _, test := range []struct {
		name, comparison          string
		actual, reported, replica int
		pass, validate            bool
	}{
		{"normal success", "false", 0, 0, 0, true, false},
		{"normal failure", "false", 23, 0, 0, false, false},
		{"complete pair observation", "true", 0, 0, 0, true, true},
		{"unexpected collector failure", "true", 23, 0, 0, false, false},
		{"honest failed baseline", "true", 23, 23, 0, true, true},
		{"misbound cohort replica", "true", 0, 0, 1, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := codeQLProfileFixture(t, test.actual, test.reported, test.replica)
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Dir = directory
			command.Stdin = strings.NewReader(entrypoint)

			command.Env = append(
				os.Environ(),
				"RUNNER_TEMP="+directory,
				"SWAP_COMPARISON="+test.comparison,
				"ADDED_SWAP_GIB=0",
				"KSAIL_CODEQL_PROFILE_REPLICA=0",
				"SOURCE_SHA="+strings.Repeat("a", 40),
			)
			output, runErr := command.CombinedOutput()
			assert.Equal(t, test.pass, runErr == nil, "%s: %v", output, runErr)

			_, statErr := os.Stat(filepath.Join(directory, "validated"))
			assert.Equal(
				t,
				test.validate,
				statErr == nil,
				"validator invocation disagrees with actual status",
			)
		})
	}
}

type codeQLWorkflowStep struct {
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

func TestCodeQLComparisonRequiresSuccessfulCollection(t *testing.T) {
	t.Parallel()

	step := codeQLProfileEntrypoint(
		t, "compare", "Compare matching environments and native database coverage",
	)

	for _, result := range []string{"success", "failure", "cancelled", "skipped", ""} {
		t.Run(result, func(t *testing.T) {
			t.Parallel()
			directory := codeQLComparisonFixture(t)
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Dir = directory
			command.Stdin = strings.NewReader(step.Run)

			command.Env = append(os.Environ(), "RUNNER_TEMP="+directory,
				"SOURCE_SHA="+strings.Repeat("a", 40), "PROFILE_JOBS_RESULT="+result)
			output, runErr := command.CombinedOutput()
			assert.Equal(t, result == "success", runErr == nil, "%s: %v", output, runErr)

			_, statErr := os.Stat(filepath.Join(directory, "combined"))
			assert.Equal(
				t,
				result == "success",
				statErr == nil,
				"untrusted collection reached the combiner",
			)
		})
	}

	require.Equal(t, "${{ needs.profile.result }}", step.Env["PROFILE_JOBS_RESULT"])
}

func codeQLComparisonFixture(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	scripts := filepath.Join(directory, ".github/scripts")
	require.NoError(t, os.MkdirAll(scripts, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(scripts, "profile-codeql-cohort.sh"),
		[]byte("printf called > \"$RUNNER_TEMP/combined\"\nprintf '{}\\n'\n"), 0o600))

	return directory
}

func codeQLProfileEntrypoint(t *testing.T, job, name string) codeQLWorkflowStep {
	t.Helper()

	var workflow struct {
		Jobs map[string]struct {
			Steps []codeQLWorkflowStep `yaml:"steps"`
		} `yaml:"jobs"`
	}

	err := yaml.Unmarshal(readRepoFile(t, ".github/workflows/codeql-go-profile.yaml"), &workflow)
	require.NoError(t, err)

	for _, step := range workflow.Jobs[job].Steps {
		if step.Name == name {
			require.NotEmpty(t, step.Run)

			return step
		}
	}

	t.Fatalf("workflow entrypoint %s/%s was not found", job, name)

	return codeQLWorkflowStep{}
}

func codeQLProfileFixture(t *testing.T, actual, reported, replica int) string {
	t.Helper()

	directory := t.TempDir()
	scripts := filepath.Join(directory, ".github/scripts")
	require.NoError(t, os.MkdirAll(scripts, 0o700))

	collector := fmt.Sprintf(
		"mkdir -p \"$1\"\nprintf '{\"profileExitCode\":%d,\"replica\":%d}\\n' > \"$1/report.json\"\nexit %d\n",
		reported,
		replica,
		actual,
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(scripts, "profile-codeql-go.sh"),
			[]byte(collector),
			0o600,
		),
	)

	validator := "printf called > \"$RUNNER_TEMP/validated\"\n"
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(scripts, "profile-codeql-compare.sh"),
			[]byte(validator),
			0o600,
		),
	)

	return directory
}

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

//nolint:tagliatelle // GitHub Actions defines this external key in kebab-case.
type releaseJob struct {
	Needs           []string          `yaml:"needs"`
	If              string            `yaml:"if"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	Permissions     map[string]string `yaml:"permissions"`
	Steps           []harnessStep     `yaml:"steps"`
}

func readReleaseJobs(t *testing.T) map[string]releaseJob {
	t.Helper()

	var workflow struct {
		Jobs map[string]releaseJob `yaml:"jobs"`
	}

	require.NoError(t, yaml.Unmarshal(readRepoFile(t, ".github/workflows/cd.yaml"), &workflow))
	require.NotEmpty(t, workflow.Jobs)

	return workflow.Jobs
}

func TestReleaseJobsRequireValidatedRef(t *testing.T) {
	t.Parallel()

	jobs := readReleaseJobs(t)
	gate, exists := jobs["validate-release-ref"]
	require.True(t, exists, "release must validate its ref before any build or mutation")
	assert.Empty(t, gate.Needs)
	assert.Empty(t, gate.If)
	assert.False(t, gate.ContinueOnError)
	assert.Equal(t, map[string]string{"contents": "read"}, gate.Permissions)

	for name, job := range jobs {
		if name == "validate-release-ref" {
			continue
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, releaseDependsOnValidation(jobs, name, map[string]bool{}),
				"release job must be downstream of ref validation")

			if name == "cleanup-failed-release" {
				assert.Contains(t, job.Needs, "validate-release-ref")
				assert.Contains(t, job.Needs, "publish-release")
				// This approved condition allows cleanup after a valid release fails,
				// but never lets always() delete a ref that failed validation.
				assert.Equal(t, "always() && needs.publish-release.result != 'success' && "+
					"needs.validate-release-ref.result == 'success'", job.If)
			} else {
				assert.Empty(t, job.If,
					"release jobs must retain the default success dependency gate")
			}
		})
	}
}

func releaseDependsOnValidation(
	jobs map[string]releaseJob,
	name string,
	visited map[string]bool,
) bool {
	if name == "validate-release-ref" {
		return true
	}

	if visited[name] {
		return false
	}

	visited[name] = true

	for _, dependency := range jobs[name].Needs {
		if releaseDependsOnValidation(jobs, dependency, visited) {
			return true
		}
	}

	return false
}

func TestReleaseRefValidationStep(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")

	gate, exists := readReleaseJobs(t)["validate-release-ref"]
	require.True(t, exists)
	require.Len(t, gate.Steps, 2, "checkout followed immediately by validation")
	assert.Contains(t, gate.Steps[0].Uses, "actions/checkout@")
	assert.Equal(t, false, gate.Steps[0].With["persist-credentials"])
	validation := gate.Steps[1]
	require.Equal(t, ".github/scripts/validate-release-ref.sh", validation.Run)
	assert.Empty(t, validation.If)
	assert.False(t, validation.ContinueOnError, "a rejected ref must fail the validation job")
	assert.Empty(t, validation.Env, "validation must read the actual runner ref")

	for _, test := range []struct {
		ref   string
		valid bool
	}{
		{ref: "refs/tags/v7.181.8", valid: true},
		{ref: "refs/tags/vanything", valid: false},
	} {
		t.Run(test.ref, func(t *testing.T) {
			t.Parallel()

			command := exec.CommandContext(
				t.Context(),
				"bash",
				".github/scripts/validate-release-ref.sh",
			)
			command.Dir = filepath.Join("..", "..")

			command.Env = append(envWithoutReleaseRef(), "GITHUB_REF="+test.ref)

			output, err := command.CombinedOutput()
			if test.valid {
				require.NoErrorf(t, err, "valid release rejected: %s", output)
			} else {
				require.Error(t, err, "malformed ref reached release work")
				assert.Contains(t, string(output), test.ref)
			}
		})
	}
}

func envWithoutReleaseRef() []string {
	var result []string

	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GITHUB_REF=") {
			result = append(result, value)
		}
	}

	return result
}

func TestReleaseRefScript(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")

	command := exec.CommandContext(
		t.Context(),
		"bash",
		"../../.github/scripts/validate-release-ref.test.sh",
	)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "release ref validation failed:\n%s", output)
}

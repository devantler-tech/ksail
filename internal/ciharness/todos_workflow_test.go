package ciharness_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// todosWorkflow captures the local job surface that delivers scanner inputs.
//
//nolint:tagliatelle // GitHub Actions defines these external keys in kebab-case.
type todosWorkflow struct {
	Jobs map[string]struct {
		Uses   string `yaml:"uses"`
		RunsOn string `yaml:"runs-on"`
		Steps  []struct {
			Env map[string]string `yaml:"env"`
			Run string            `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestTODOScannerExcludesOnlyVendoredSources(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/todos.yaml")

	var workflow todosWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	job, found := workflow.Jobs["todos"]
	require.True(t, found, "TODO workflow must define the todos job")
	require.Empty(t, job.Uses, "TODO scanner inputs must be delivered by the KSail-owned job")
	require.Equal(t, "ubuntu-latest", job.RunsOn)

	var (
		scanEnv map[string]string
		scanRun string
	)

	for _, step := range job.Steps {
		if _, configured := step.Env["INPUT_IGNORE"]; configured {
			scanEnv = step.Env
			scanRun = step.Run

			break
		}
	}

	require.NotNil(t, scanEnv, "scanner step must configure INPUT_IGNORE")

	ignorePattern := scanEnv["INPUT_IGNORE"]
	assert.Equal(t, `^third_party/`, ignorePattern)

	compiled, err := regexp.Compile(ignorePattern)
	require.NoError(t, err)
	assert.True(t, compiled.MatchString("third_party/go-archive/archive/tar/reader.go"))
	assert.False(t, compiled.MatchString("internal/maintenance/todos.go"))

	assert.Contains(t, strings.Join(strings.Fields(scanRun), " "), "--env INPUT_IGNORE")
	assert.Equal(
		t,
		"ghcr.io/alstr/todo-to-issue-action:v5.1.15@sha256:"+
			"a1794cab7e7a0306d6f74dd5249734f2c0b49e47d7549c6cdca4421bced67c9d",
		scanEnv["TODO_TO_ISSUE_IMAGE"],
	)
}

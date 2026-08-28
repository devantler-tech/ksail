package ciharness_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// todosWorkflow captures the reusable job surface that delivers scanner inputs.
//
//nolint:tagliatelle // GitHub Actions defines these external keys in kebab-case.
type todosWorkflow struct {
	Jobs map[string]struct {
		Uses    string            `yaml:"uses"`
		RunsOn  string            `yaml:"runs-on"`
		Steps   []map[string]any  `yaml:"steps"`
		With    map[string]string `yaml:"with"`
		Secrets map[string]string `yaml:"secrets"`
	} `yaml:"jobs"`
}

func TestTODOScannerExcludesOnlyVendoredSources(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/todos.yaml")

	var workflow todosWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	job, found := workflow.Jobs["todos"]
	require.True(t, found, "TODO workflow must define the todos job")
	require.Regexp(
		t,
		`^devantler-tech/actions/\.github/workflows/scan-for-todo-comments\.yaml@[0-9a-f]{40}$`,
		job.Uses,
		"shared scanner must use the reviewed workflow pinned to an immutable commit",
	)
	assert.Empty(t, job.RunsOn, "reusable workflow callers cannot configure runs-on")
	assert.Empty(t, job.Steps, "KSail must not copy the shared scanner implementation")
	assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", job.Secrets["APP_PRIVATE_KEY"])

	ignorePattern := job.With["ignore"]
	assert.Equal(t, `^third_party/`, ignorePattern)

	compiled, err := regexp.Compile(ignorePattern)
	require.NoError(t, err)
	assert.True(t, compiled.MatchString("third_party/go-archive/archive/tar/reader.go"))
	assert.False(t, compiled.MatchString("internal/maintenance/todos.go"))
	assert.Regexp(t, `# v\d+\.\d+\.\d+`, string(contents), "workflow pin must retain its release annotation")
}

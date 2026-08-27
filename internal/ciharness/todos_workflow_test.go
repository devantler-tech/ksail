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
	require.Equal(
		t,
		"devantler-tech/actions/.github/workflows/scan-for-todo-comments.yaml@"+
			"56665ea3f455622d6ff0cc0fb2b90fc3be6e41e7",
		job.Uses,
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
	assert.Contains(t, string(contents), "# v13.2.2")
}

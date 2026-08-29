package ciharness_test

import (
	"regexp"
	"strings"
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

// Pins the shared scanner's delegation and the blast radius of its ignore pattern.
//
// This comment deliberately does not open with the function's own name, the way a doc comment
// normally would: that name embeds the uppercase marker, and the scanner keys on the marker
// wherever it appears in a comment. See TestCIFilterCommentDoesNotSpellTheScannerMarker below,
// which pins the same trap after it fired once for real.
//
// KSail calls the shared scanner in devantler-tech/actions instead of carrying its own copy, so the
// job must stay a bare reusable-workflow call: a runs-on or steps key here would mean the
// implementation had been forked and could drift from the reviewed one. The pin is required to be an
// immutable 40-character commit that keeps its release annotation, so the workflow KSail runs is
// always the workflow that was reviewed.
//
// The ignore pattern is asserted exactly rather than loosely, because widening it fails silently: a
// pattern that also swallowed first-party paths would leave real to-do comments unscanned with no
// check going red anywhere. third_party/ is vendored source KSail does not own; internal/ is its own
// code and must stay covered.
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
	assert.Regexp(
		t,
		`(?m)^\s*uses:\s+devantler-tech/actions/\.github/workflows/`+
			`scan-for-todo-comments\.yaml@[0-9a-f]{40}\s+# v\d+\.\d+\.\d+\s*$`,
		string(contents),
		"workflow pin must retain its release annotation",
	)
}

// TestCIFilterCommentDoesNotSpellTheScannerMarker pins a trap that has already fired once.
//
// The shared scanner keys on the uppercase marker wherever it appears in a comment, so prose
// that merely NAMES the marker files an issue against itself. ksail#6763 was exactly that: a
// false positive whose entire body was the ci.yaml comment block below, with no work behind
// it. The shared workflow in devantler-tech/actions guards itself the same way and says so in
// its own source.
//
// The marker is assembled at runtime rather than written out, because spelling it in this
// file would reintroduce the very defect the test exists to prevent.
func TestCIFilterCommentDoesNotSpellTheScannerMarker(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/ci.yaml")
	lines := strings.Split(string(contents), "\n")

	anchor := -1

	for i, line := range lines {
		if strings.TrimSpace(line) == "todos-contract:" {
			anchor = i

			break
		}
	}

	require.GreaterOrEqual(t, anchor, 0, "ci.yaml must still define the todos-contract filter")

	// Walk back over the contiguous comment block that documents the filter. Asserting the
	// block is non-empty stops this test passing vacuously if the comment is ever deleted.
	var block []string

	for i := anchor - 1; i >= 0 && strings.HasPrefix(strings.TrimSpace(lines[i]), "#"); i-- {
		block = append(block, lines[i])
	}

	require.NotEmpty(t, block, "the todos-contract filter must keep its explanatory comment")

	marker := "TO" + "DO"
	for _, line := range block {
		assert.NotContains(
			t,
			line,
			marker,
			"comment must not spell the scanner marker: doing so files a spurious issue (ksail#6763)",
		)
	}
}

package ciharness_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// harnessPathLiteral matches a quoted repository path under the three .github trees this
// package reads: workflows, composite actions and scripts. Glob characters are excluded so a
// pattern written in a test (like the filter entries below) is never mistaken for a read path.
var harnessPathLiteral = regexp.MustCompile(
	`"(\.github/(?:workflows|actions|scripts)/[^"*?\[\]{}]+)"`,
)

// TestWorkflowContractFilterCoversEveryFileTheHarnessReads keeps the CI gate for this package
// from drifting away from what the package actually checks.
//
// The contract job in ci.yaml only runs when its todos-contract path filter matches. When a
// file these tests read is missing from that filter, a PR editing only that file skips the
// tests that guard it, and the aggregator records the skip as success. ksail#7008 hit exactly
// that: it raised the EKS smoke job timeout in system-test-eks.yaml alone, broke
// TestEKSSmokeReservesCleanupBudgetAndFreshCredentials, and passed CI (ksail#7009).
//
// "Read" covers both ways the package reaches a file: every workflow, because
// TestNoDefaultBranchWorkflowCancelsRunsInProgress globs the whole workflows directory, and
// every quoted workflow, action or script path in these test sources that exists on disk.
func TestWorkflowContractFilterCoversEveryFileTheHarnessReads(t *testing.T) {
	t.Parallel()

	patterns := workflowContractFilterPatterns(t)
	readPaths := harnessReadPaths(t)

	// Guard the guard: if discovery ever returned nothing, the loop below would assert nothing.
	require.Contains(
		t,
		readPaths,
		".github/workflows/system-test-eks.yaml",
		"path discovery must find the workflows this package is known to read",
	)

	var uncovered []string

	for _, path := range readPaths {
		if !contractFilterMatches(t, patterns, path) {
			uncovered = append(uncovered, path)
		}
	}

	assert.Emptyf(
		t,
		uncovered,
		"the todos-contract filter in ci.yaml must match every file internal/ciharness reads, "+
			"or a PR editing only that file skips these tests; add a pattern covering: %s",
		strings.Join(uncovered, ", "),
	)

	// The matcher must be able to say no, or the assertion above could never fail.
	withoutWorkflows := slices.DeleteFunc(slices.Clone(patterns), func(pattern string) bool {
		return pattern == ".github/workflows/**"
	})
	assert.False(
		t,
		contractFilterMatches(t, withoutWorkflows, ".github/workflows/system-test-eks.yaml"),
		"negative control: without the workflows pattern the EKS workflow must be uncovered",
	)
}

// workflowContractFilterPatterns returns the todos-contract entries from the changes job.
func workflowContractFilterPatterns(t *testing.T) []string {
	t.Helper()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	changesJob, found := workflow.Jobs["changes"]
	require.True(t, found, "changes job is missing")

	filterStep := findHarnessStep(t, changesJob.Steps, "🔍 Filter paths")

	var filters map[string][]string
	require.NoError(t, yaml.Unmarshal([]byte(stringValue(filterStep.With["filters"])), &filters))

	patterns := filters["todos-contract"]
	require.NotEmpty(t, patterns, "ci.yaml must define a non-empty todos-contract filter")

	return patterns
}

// harnessReadPaths lists the repository files this package reads, sorted and de-duplicated.
func harnessReadPaths(t *testing.T) []string {
	t.Helper()

	workflows, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.y*ml"))
	require.NoError(t, err)
	require.NotEmpty(t, workflows)

	paths := make([]string, 0, len(workflows))
	for _, workflow := range workflows {
		paths = append(paths, filepath.ToSlash(strings.TrimPrefix(workflow, "../../")))
	}

	sources, err := filepath.Glob("*_test.go")
	require.NoError(t, err)
	require.NotEmpty(t, sources)

	// Resolve literals through an fs.FS rooted at the repository: fs.Stat rejects any path
	// that is not a valid, unrooted, dot-dot-free name, so a literal can never reach a file
	// outside the repository even though it comes from file contents.
	repo := os.DirFS(filepath.Join("..", ".."))

	for _, source := range sources {
		// The glob supplies this package's own test sources, never user input.
		contents, readErr := os.ReadFile(source) //nolint:gosec
		require.NoError(t, readErr)

		for _, match := range harnessPathLiteral.FindAllStringSubmatch(string(contents), -1) {
			_, statErr := fs.Stat(repo, match[1])
			if statErr == nil {
				paths = append(paths, match[1])
			}
		}
	}

	slices.Sort(paths)

	return slices.Compact(paths)
}

// contractFilterMatches reports whether any pattern covers path. It supports exactly the two
// forms the filter uses, an exact path or a "dir/**" prefix, and fails on any other glob so a
// pattern it cannot evaluate is never silently treated as a miss or a match.
func contractFilterMatches(t *testing.T, patterns []string, path string) bool {
	t.Helper()

	for _, pattern := range patterns {
		prefix, isTree := strings.CutSuffix(pattern, "/**")
		require.False(
			t,
			strings.ContainsAny(prefix, "*?[]{}!"),
			"unsupported todos-contract pattern %q: use an exact path or a dir/** prefix",
			pattern,
		)

		if pattern == path || (isTree && strings.HasPrefix(path, prefix+"/")) {
			return true
		}
	}

	return false
}

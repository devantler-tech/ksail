package ciharness_test

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docsTestsPattern is the docs filter entry that selects the documentation's own tests.
const docsTestsPattern = "docs/tests/**"

// knownDocsTestPath guards discovery: the docs build runs this test today, so a walk that
// does not find it has looked in the wrong place.
const knownDocsTestPath = "docs/tests/" + "dependency-compatibility.test.mjs"

// TestDocsFilterSelectsEveryDocsTest keeps the docs build selected for a change to any
// documentation test.
//
// The docs build is the only CI job that runs the tests under docs/tests: npm run build
// runs them before Astro builds the site, and the job runs only when ci.yaml's docs path
// filter matches. A test the filter does not match merges without ever running, while the
// aggregator records the skipped job as success (ksail#7836).
func TestDocsFilterSelectsEveryDocsTest(t *testing.T) {
	t.Parallel()

	patterns := changesFilterPatterns(t, "docs")
	tests := docsTestPaths(t)

	require.Contains(t, tests, knownDocsTestPath,
		"docs test discovery must find the tests the docs build is known to run")

	var unselected []string

	for _, test := range tests {
		if !pathFilterMatches(t, patterns, test) {
			unselected = append(unselected, test)
		}
	}

	assert.Emptyf(
		t,
		unselected,
		"the docs filter in ci.yaml must match every file under docs/tests, or a PR changing "+
			"only that file skips the docs build that runs it; add %q to the filter. Unselected: %s",
		docsTestsPattern,
		strings.Join(unselected, ", "),
	)

	// The matcher must be able to say no, or the assertion above could never fail.
	withoutTests := slices.DeleteFunc(slices.Clone(patterns), func(pattern string) bool {
		return pattern == docsTestsPattern
	})
	assert.False(
		t,
		pathFilterMatches(t, withoutTests, knownDocsTestPath),
		"negative control: without the docs tests pattern a docs test must be unselected",
	)
}

// TestPathFilterMatches pins the matcher's reading of the two pattern shapes ci.yaml's
// filters use, so a matcher bug cannot pass the coverage check above vacuously.
func TestPathFilterMatches(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"tree pattern matches a direct child", "docs/tests/**", "docs/tests/a.test.mjs", true},
		{"tree pattern matches a nested file", "docs/tests/**", "docs/tests/sub/a.test.mjs", true},
		{"tree pattern rejects a sibling prefix", "docs/tests/**", "docs/testsuite/a.mjs", false},
		{"tree pattern rejects its own directory", "docs/tests/**", "docs/tests", false},
		{"segment glob matches", "docs/package*.json", "docs/package-lock.json", true},
		{"segment glob rejects another directory", "docs/package*.json", "docs/src/package.json", false},
		{"exact path matches", ".github/workflows/ci.yaml", ".github/workflows/ci.yaml", true},
		{"exact path rejects another file", ".github/workflows/ci.yaml", ".github/workflows/cd.yaml", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, pathFilterMatches(t, []string{test.pattern}, test.path))
		})
	}
}

// pathFilterMatches reports whether any paths-filter pattern selects filePath. It reads the two
// shapes ci.yaml's filters use: a dir/** tree, and a path whose segments may hold * and ?
// globs. Any other use of ** fails the test rather than being guessed at.
func pathFilterMatches(t *testing.T, patterns []string, filePath string) bool {
	t.Helper()

	for _, pattern := range patterns {
		prefix, isTree := strings.CutSuffix(pattern, "/**")
		require.NotContainsf(
			t,
			prefix,
			"**",
			"unsupported path filter pattern %q: use a dir/** tree or single-segment globs",
			pattern,
		)
		require.NotContainsf(t, pattern, "!",
			"unsupported path filter pattern %q: negative patterns are not read here", pattern)

		if isTree {
			if strings.HasPrefix(filePath, prefix+"/") {
				return true
			}

			continue
		}

		matched, err := path.Match(pattern, filePath)
		require.NoErrorf(t, err, "malformed path filter pattern %q", pattern)

		if matched {
			return true
		}
	}

	return false
}

// docsTestPaths lists every file under docs/tests as a repository-relative path.
func docsTestPaths(t *testing.T) []string {
	t.Helper()

	repo := os.DirFS(filepath.Join("..", ".."))

	var paths []string

	err := fs.WalkDir(
		repo,
		"docs/tests",
		func(entryPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if !entry.IsDir() {
				paths = append(paths, entryPath)
			}

			return nil
		},
	)
	require.NoError(t, err)

	return paths
}

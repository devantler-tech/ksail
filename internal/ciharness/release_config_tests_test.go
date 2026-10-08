package ciharness_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// releaseConfigFiles lists the release build configuration that Go tests take as their subject:
// both GoReleaser files, the CD workflow that carries the desktop release job, the setup action
// that job uses, and the scripts the release builds run. pkg/client/kubescape pins a digest of
// every one of them (auditedReleaseConfigDigests), so editing any of them can turn a Go test red.
func releaseConfigFiles() []string {
	return []string{
		".goreleaser.yaml",
		".goreleaser.desktop.yaml",
		".github/workflows/cd.yaml",
		".github/scripts/publish-operator-chart.sh",
		".github/actions/setup-desktop-build/action.yml",
		".github/actions/free-disk-space/free-disk-space.sh",
		"scripts/stage-webui.sh",
	}
}

// TestReleaseConfigFilterCoversTheReleaseFiles keeps a pull request that changes only the
// release build configuration from merging with the Go tests about it unexecuted (ksail#7400).
//
// The org-required Go validation gates its test job on Go sources and module files, so on such
// a pull request it skips the suite by its own rule, and the mismatch surfaces later on an
// unrelated Go change. ci.yaml therefore runs the suite itself whenever its release-config
// filter matches; this test pins what that filter matches.
func TestReleaseConfigFilterCoversTheReleaseFiles(t *testing.T) {
	t.Parallel()

	patterns := changesFilterPatterns(t, "release-config")

	for _, path := range releaseConfigFiles() {
		_, err := os.Stat(filepath.Join("..", "..", path))
		require.NoErrorf(t, err, "%s no longer exists: update releaseConfigFiles", path)
		assert.Truef(
			t,
			contractFilterMatches(t, patterns, path),
			"the release-config filter in ci.yaml must match %s, or a pull request editing only "+
				"that file skips the Go tests that assert on it",
			path,
		)
	}

	// The gate lives in ci.yaml, so a pull request that edits only ci.yaml must still run it.
	assert.True(
		t,
		contractFilterMatches(t, patterns, ".github/workflows/ci.yaml"),
		"the release-config filter must include ci.yaml, which defines the gated job",
	)

	// The matcher must be able to say no, or the assertions above could never fail.
	assert.False(
		t,
		contractFilterMatches(t, patterns, "README.md"),
		"negative control: the release-config filter must not match unrelated files",
	)
}

// TestReleaseConfigChangesRunTheGoTests pins the job the release-config filter feeds: it runs
// the whole Go suite on a matching pull request, and the required-checks aggregator counts its
// result, so a red run blocks the merge instead of being recorded as an ordinary skip.
func TestReleaseConfigChangesRunTheGoTests(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")

	assert.Contains(
		t,
		changesJobOutputs(t)["release-config"],
		"steps.filter.outputs.release-config",
		"the changes job must publish the release-config filter result",
	)

	job, found := workflow.Jobs["test-release-config"]
	require.True(t, found, "test-release-config job is missing")
	assert.Contains(t, job.Needs, "changes")
	assert.Contains(t, job.If, "github.event_name == 'pull_request'")
	assert.Contains(t, job.If, "needs.changes.outputs.release-config == 'true'")

	// -count=1 is load-bearing: the suite's subject here is files the Go build graph cannot see,
	// so a cached PASS from an earlier run would be replayed over the changed configuration.
	testStep := findHarnessStep(t, job.Steps, "🧪 Test")
	assert.Contains(t, testStep.Run, "go test ./... -count=1")

	aggregator, found := workflow.Jobs["require-checks-in-pr"]
	require.True(t, found, "require-checks-in-pr job is missing")
	assert.True(
		t,
		slices.Contains(aggregator.Needs, "test-release-config"),
		"the required-checks aggregator must wait for test-release-config",
	)

	aggregateStep := aggregator.Steps[len(aggregator.Steps)-1]
	assert.Contains(
		t,
		stringValue(aggregateStep.With["job-results"]),
		"needs.test-release-config.result",
		"the required-checks aggregator must count test-release-config's result",
	)
}

// changesJobOutputs returns the outputs ci.yaml's changes job publishes.
func changesJobOutputs(t *testing.T) map[string]string {
	t.Helper()

	var workflow struct {
		Jobs map[string]struct {
			Outputs map[string]string `yaml:"outputs"`
		} `yaml:"jobs"`
	}

	require.NoError(t, yaml.Unmarshal(readRepoFile(t, ".github/workflows/ci.yaml"), &workflow))

	changesJob, found := workflow.Jobs["changes"]
	require.True(t, found, "changes job is missing")

	return changesJob.Outputs
}

package ciharness_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// concurrencyWorkflow captures the trigger and concurrency surface that decides
// whether a run on the default branch can be evicted by a later one.
//
//nolint:tagliatelle // GitHub Actions defines these external keys in kebab-case.
type concurrencyWorkflow struct {
	On struct {
		Push struct {
			Branches []string `yaml:"branches"`
		} `yaml:"push"`
	} `yaml:"on"`
	Concurrency struct {
		Group            string `yaml:"group"`
		CancelInProgress any    `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
}

func TestCIWorkflowKeepsDefaultBranchRunsAlive(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/ci.yaml")

	var workflow concurrencyWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	// The group stays ref-keyed so superseded pull-request runs still cancel:
	// each pull request carries its own refs/pull/<n>/merge ref, and each merge
	// queue entry its own gh-readonly-queue ref.
	assert.Contains(t, workflow.Concurrency.Group, "github.ref")

	// On main the group must be run-unique. cancel-in-progress alone does not
	// make a shared group safe there: a concurrency group holds one running plus
	// one pending run, and a third push cancels the pending one — so the second
	// push's checks are lost even with cancellation disabled.
	assert.Containsf(
		t,
		workflow.Concurrency.Group,
		"github.run_id",
		"main must get a run-unique concurrency group, else a queued run is evicted by the next push",
	)
	assert.Contains(t, workflow.Concurrency.Group, "refs/heads/main")

	// A ref-keyed group gives every push to main the same key, so cancelling
	// unconditionally evicts the previous merge's checks mid-flight. main then
	// reports green on verification that never finished.
	cancel, isExpression := workflow.Concurrency.CancelInProgress.(string)
	require.Truef(
		t,
		isExpression,
		"cancel-in-progress must be an expression excluding the default branch, got %#v",
		workflow.Concurrency.CancelInProgress,
	)

	assert.Contains(t, cancel, "github.ref")
	assert.Contains(t, cancel, "refs/heads/main")
	assert.Contains(t, cancel, "!=")
}

func TestNoDefaultBranchWorkflowCancelsRunsInProgress(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.y*ml"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	checked := 0

	for _, path := range paths {
		// The glob supplies repository-owned workflow paths, never user input.
		contents, readErr := os.ReadFile(path) //nolint:gosec
		require.NoError(t, readErr)

		var workflow concurrencyWorkflow
		require.NoErrorf(t, yaml.Unmarshal(contents, &workflow), "parsing %s", path)

		if !slices.Contains(workflow.On.Push.Branches, "main") {
			continue
		}

		checked++

		// Both the YAML boolean and a quoted "true" are truthy to GitHub, so
		// compare on the rendered value rather than the parsed type.
		cancels := fmt.Sprintf("%v", workflow.Concurrency.CancelInProgress) == "true"

		assert.Falsef(
			t,
			cancels,
			"%s runs on main and cancels in progress unconditionally, so one merge evicts "+
				"the previous merge's checks before they complete",
			filepath.Base(path),
		)
	}

	// Guard the guard: were the glob or the trigger shape to stop matching, the
	// loop above would pass over an empty set and assert nothing at all. Five
	// workflows push to main today (ci, desktop, release, todos, web-ui); the
	// floor stays low so removing one does not fail this test spuriously.
	assert.GreaterOrEqualf(
		t,
		checked,
		2,
		"expected several workflows to trigger on push to main, matched %d",
		checked,
	)
}

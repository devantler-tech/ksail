package ciharness_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	//
	// Assert the whole conditional rather than the tokens separately. Checking
	// "github.run_id", "refs/heads/main" and "github.ref" as independent
	// substrings also passes for the INVERTED expression
	//   github.ref != 'refs/heads/main' && github.run_id || github.ref
	// which hands main the shared ref key and reinstates the eviction.
	assert.Containsf(
		t,
		workflow.Concurrency.Group,
		"github.ref == 'refs/heads/main' && github.run_id",
		"main must be bound to the run-unique key; an inverted condition gives main the shared ref key",
	)

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

		assert.Falsef(
			t,
			cancelsUnconditionally(workflow.Concurrency.CancelInProgress),
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

// cancelsUnconditionally reports whether a cancel-in-progress value cancels runs
// on every branch. A literal `true` is the obvious form, but GitHub also accepts
// an expression, and YAML hands those back as strings — so `${{ true }}` renders
// as a non-"true" string while still cancelling everything. An expression that
// never mentions github.ref cannot be branch-conditional either, so it is
// treated as unconditional rather than assumed safe.
func cancelsUnconditionally(value any) bool {
	rendered := strings.TrimSpace(fmt.Sprintf("%v", value))
	if rendered == "true" {
		return true
	}

	if !strings.HasPrefix(rendered, "${{") || !strings.HasSuffix(rendered, "}}") {
		return false
	}

	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(rendered, "${{"), "}}"))

	return inner == "true" || !strings.Contains(inner, "github.ref")
}

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

	// Hold cancel-in-progress to the same allowlist the repo-wide test uses.
	// Substring checks would be misleading here for exactly the reason they were
	// wrong there: `${{ github.ref != 'refs/heads/main' || true }}` contains
	// "github.ref", "refs/heads/main" and "!=" while still cancelling on main.
	assert.Equalf(
		t,
		approvedCancelExpression,
		workflow.Concurrency.CancelInProgress,
		"cancel-in-progress must be exactly the approved expression",
	)
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
			mayCancelDefaultBranchRuns(workflow.Concurrency.CancelInProgress),
			"%s runs on main and may cancel in progress there, so one merge evicts the "+
				"previous merge's checks before they complete (allowed: absent, false, or %s)",
			filepath.Base(path),
			approvedCancelExpression,
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

// approvedCancelExpression is the only expression form allowed to gate
// cancellation on a workflow that runs on the default branch.
const approvedCancelExpression = "${{ github.ref != 'refs/heads/main' }}"

// mayCancelDefaultBranchRuns reports whether a cancel-in-progress value could
// cancel a run on main. It FAILS CLOSED: only an absent value, an explicit
// false, or the exact approved expression are accepted.
//
// Inspecting the tokens of an arbitrary expression does not work, because what
// matters is the value it evaluates to when github.ref is refs/heads/main —
// which a substring check cannot tell. Both `${{ github.ref && true }}` and
// `${{ github.ref != 'refs/heads/main' || true }}` mention github.ref and look
// branch-conditional, yet both evaluate true on main and cancel the previous
// run. Evaluating GitHub expressions here would mean shipping an interpreter
// that itself needs testing, so an allowlist is the honest guard: a new form is
// added deliberately, with its behaviour on main reasoned about once.
func mayCancelDefaultBranchRuns(value any) bool {
	if value == nil {
		return false
	}

	switch strings.TrimSpace(fmt.Sprintf("%v", value)) {
	case "false", approvedCancelExpression:
		return false
	default:
		return true
	}
}

func TestMayCancelDefaultBranchRuns(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value any
		want  bool
	}{
		"absent":              {value: nil, want: false},
		"explicit false":      {value: false, want: false},
		"approved expression": {value: approvedCancelExpression, want: false},

		"literal true":    {value: true, want: true},
		"quoted true":     {value: "true", want: true},
		"expression true": {value: "${{ true }}", want: true},

		// Both mention github.ref and read as branch-conditional, yet each
		// evaluates true when github.ref is refs/heads/main.
		"truthy ref conjunction": {value: "${{ github.ref && true }}", want: true},
		"disjunction with true": {
			value: "${{ github.ref != 'refs/heads/main' || true }}",
			want:  true,
		},

		// Fail closed on anything not explicitly approved, including forms that
		// may well be correct — adding one is a deliberate act.
		"unreviewed variant": {
			value: "${{ github.ref != format('refs/heads/{0}', 'main') }}",
			want:  true,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, mayCancelDefaultBranchRuns(testCase.value))
		})
	}
}

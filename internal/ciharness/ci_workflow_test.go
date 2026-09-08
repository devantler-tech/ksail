package ciharness_test

import (
	"fmt"
	"os"
	"path/filepath"
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
	On          map[string]any `yaml:"on"`
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

	// Assert the whole group expression, not fragments of it. Every partial form
	// tried here has admitted a bypass: independent substrings admit the inverted
	// conditional, and asserting only the true-branch admits
	//   github.ref == 'refs/heads/main' && github.run_id || github.run_id
	// whose fallback gives pull-request and merge-queue runs unique groups too,
	// so superseded runs there can no longer cancel each other.
	assert.Equalf(
		t,
		approvedGroupExpression,
		workflow.Concurrency.Group,
		"concurrency group must be exactly the approved expression",
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

		if !runsOnDefaultBranch(workflow.On) {
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
	// loop above would pass over an empty set and assert nothing at all. Six
	// workflows can run on main today — ci, desktop, release, todos and web-ui
	// name it literally, and copilot-setup-steps.yml reaches it through an
	// unfiltered push, which a literal-name check silently skipped. The floor
	// stays low so removing one does not fail this test spuriously.
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

// approvedGroupExpression is the only concurrency group allowed for ci.yaml. It
// gives main a run-unique key while leaving every other ref — pull requests and
// merge-queue entries — sharing the ref-keyed group so superseded runs cancel.
const approvedGroupExpression = "ci-ksail-${{ github.workflow }}-" +
	"${{ github.ref == 'refs/heads/main' && github.run_id || github.ref }}"

// runsOnDefaultBranch reports whether a workflow's triggers admit a push to
// main. It FAILS OPEN toward inclusion: anything that might run there is
// checked, because a workflow wrongly skipped is a silent hole, while a
// workflow wrongly included merely has to satisfy the same cancellation rule.
//
// A literal branch list is not sufficient to decide this. An `on: push` with no
// branch filter runs on every branch; `branches-ignore` selects by exclusion;
// and both filters accept glob patterns, so `main` can be matched by `ma*n`,
// `*` or `**` without appearing literally.
func runsOnDefaultBranch(on map[string]any) bool {
	raw, present := on["push"]
	if !present {
		return false
	}

	filters, isMapping := raw.(map[string]any)
	if !isMapping {
		// `push:` with no body runs on every branch.
		return true
	}

	if ignore := patternList(filters["branches-ignore"]); len(ignore) > 0 {
		// Inverted sense: a pattern we cannot read must NOT be taken to exclude
		// main, or an unreadable ignore-pattern would skip the workflow — the
		// same silent hole, arrived at from the other side.
		return !matchesDefaultBranch(ignore, false)
	}

	branches := patternList(filters["branches"])
	if len(branches) == 0 {
		// Only tag filters, or none at all. A tag-only push never targets a
		// branch; anything else reaches every branch.
		_, tagOnly := filters["tags"]
		if !tagOnly {
			_, tagOnly = filters["tags-ignore"]
		}

		return !tagOnly
	}

	return matchesDefaultBranch(branches, true)
}

func patternList(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	patterns := make([]string, 0, len(items))
	for _, item := range items {
		patterns = append(patterns, fmt.Sprintf("%v", item))
	}

	return patterns
}

// matchesDefaultBranch reports whether any pattern selects main. whenUnparseable
// is the answer for a pattern Go's matcher rejects, and differs by caller: an
// inclusion list should assume it matches, an exclusion list should assume it
// does not, so that either way the workflow stays in the checked set.
func matchesDefaultBranch(patterns []string, whenUnparseable bool) bool {
	for _, pattern := range patterns {
		if pattern == "main" || strings.Contains(pattern, "**") {
			return true
		}

		// filepath.Match covers *, ? and character classes; branch names here
		// carry no slash, so its separator handling does not matter.
		//
		// A malformed pattern, or one using syntax GitHub accepts and Go does
		// not, errors here. Treat that as a match: this function fails open
		// toward inclusion, and silently skipping a pattern we cannot read is
		// precisely the hole this matcher exists to close.
		matched, err := filepath.Match(pattern, "main")
		if err != nil {
			if whenUnparseable {
				return true
			}

			continue
		}

		if matched {
			return true
		}
	}

	return false
}

func TestRunsOnDefaultBranch(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		yaml string
		want bool
	}{
		"literal main":     {yaml: "on:\n  push:\n    branches: [main]\n", want: true},
		"no push trigger":  {yaml: "on:\n  pull_request:\n", want: false},
		"tags only":        {yaml: "on:\n  push:\n    tags: ['v*']\n", want: false},
		"other branch":     {yaml: "on:\n  push:\n    branches: [develop]\n", want: false},
		"unfiltered push":  {yaml: "on:\n  push:\n", want: true},
		"glob star":        {yaml: "on:\n  push:\n    branches: ['*']\n", want: true},
		"glob doublestar":  {yaml: "on:\n  push:\n    branches: ['**']\n", want: true},
		"glob prefix":      {yaml: "on:\n  push:\n    branches: ['ma*n']\n", want: true},
		"ignore other":     {yaml: "on:\n  push:\n    branches-ignore: [develop]\n", want: true},
		"ignore main":      {yaml: "on:\n  push:\n    branches-ignore: [main]\n", want: false},
		"ignore main glob": {yaml: "on:\n  push:\n    branches-ignore: ['mai?']\n", want: false},

		// A pattern Go's matcher cannot parse must not silently skip the
		// workflow; inclusion is the safe direction.
		"unparseable pattern": {yaml: "on:\n  push:\n    branches: ['[main']\n", want: true},
		"unparseable ignore": {
			yaml: "on:\n  push:\n    branches-ignore: ['[main']\n",
			want: true,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var workflow concurrencyWorkflow
			require.NoError(t, yaml.Unmarshal([]byte(testCase.yaml), &workflow))
			assert.Equal(t, testCase.want, runsOnDefaultBranch(workflow.On))
		})
	}
}

// autoCommitWorkflow captures the auto-commit job's step surface, which decides
// whether CI pushes a generated-file commit onto a pull request's own branch.
type autoCommitWorkflow struct {
	Jobs map[string]struct {
		Steps []map[string]any `yaml:"steps"`
	} `yaml:"jobs"`
}

// The exact conditions the two auto-commit delivery paths must carry, plus the
// restriction that decides what may be applied on a Dependabot branch.
//
// Dependabot's exclusion lives in 📋 Apply patches rather than on the PR-branch push:
// what may reach a Dependabot branch is a per-patch question, not a per-step one, and
// pinning it on the push could only express "all patches or none". The protected-branch
// path is asserted alongside because it is where a skipped generated-file sync lands
// instead, and silently losing it would turn this into a generated-file correctness
// regression on the default branch.
const (
	approvedAutoCommitPRBranchCondition        = "github.event_name == 'pull_request'"
	approvedAutoCommitProtectedBranchCondition = "github.event_name != 'pull_request'"
	approvedDesktopTidyOnlyRestriction         = "${{ github.event_name == 'pull_request'" +
		" && github.event.pull_request.user.login == 'dependabot[bot]' }}"
)

// The guard 📋 Apply patches must carry, pinned as a whole block.
//
// A substring match on the variable name alone would pass against an inverted test, a
// negated comparison, or a guard that logs without skipping — each of which pushes a
// generated-file commit onto a Dependabot branch and re-opens #6832. Requiring the
// `continue` in the same block is what makes this an assertion about behaviour.
const approvedDesktopTidyOnlyGuard = `if [ "${DESKTOP_TIDY_ONLY}" = "true" ] && ` +
	`[ "$(basename "$patch")" != "desktop-tidy.patch" ]; then`

// stepCondition returns the `if:` of the uniquely-named step in the given job.
//
// It requires exactly one match rather than taking the first: two steps sharing a
// name would make the assertion below silently describe whichever came first.
func stepCondition(t *testing.T, job []map[string]any, stepName string) string {
	t.Helper()

	var found []map[string]any

	for _, step := range job {
		if name, ok := step["name"].(string); ok && name == stepName {
			found = append(found, step)
		}
	}

	require.Lenf(t, found, 1, "expected exactly one step named %q", stepName)

	condition, ok := found[0]["if"].(string)
	require.Truef(t, ok, "step %q must carry a string `if:` condition", stepName)

	return condition
}

// step returns the uniquely-named step in the given job.
//
// Same uniqueness requirement as stepCondition, and for the same reason: two steps
// sharing a name would make the caller silently assert against whichever came first.
func step(t *testing.T, job []map[string]any, stepName string) map[string]any {
	t.Helper()

	var found []map[string]any

	for _, candidate := range job {
		if name, ok := candidate["name"].(string); ok && name == stepName {
			found = append(found, candidate)
		}
	}

	require.Lenf(t, found, 1, "expected exactly one step named %q", stepName)

	return found[0]
}

// Pins Dependabot's ownership of its own branches.
//
// GitHub permanently revokes Dependabot's ability to rebase a pull request once any
// other actor pushes to its branch ("this PR has been edited by someone other than
// Dependabot"). CI's generated-file sync used to push onto every same-repo PR branch,
// Dependabot's included, so a synced bump could never recover from base drift: the
// moment anything else merged it went behind base, auto-merge could never fire, and
// the ordinary `@dependabot rebase` remedy was refused. Roughly half of Dependabot
// PRs carried the sync commit, and because the ecosystem is capped at an open-PR
// limit, the stranded ones throttled dependency intake generally (issue #6832).
//
// The condition is asserted whole rather than by substring for the reason the
// concurrency tests above record: a fragment match admits inverted and appended
// forms that still push to Dependabot's branch. A generated-file change on a
// Dependabot bump is not lost by this exclusion — it lands through the
// protected-branch path after the bump merges, which is why that path is pinned here
// too.
func TestAutoCommitLeavesDependabotBranchesToDependabot(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/ci.yaml")

	var workflow autoCommitWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	job, found := workflow.Jobs["auto-commit"]
	require.True(t, found, "ci workflow must define the auto-commit job")

	assert.Equal(
		t,
		approvedAutoCommitPRBranchCondition,
		stepCondition(t, job.Steps, "📤 Commit and push generated changes (PR branch)"),
		"the PR-branch push must commit whatever was applied; restricting WHICH patches"+
			" reach a Dependabot branch is 📋 Apply patches' job, asserted below",
	)

	assert.Equal(
		t,
		approvedAutoCommitProtectedBranchCondition,
		stepCondition(t, job.Steps, "📤 Open PR for generated changes (protected branch)"),
		"the protected-branch sync must stay reachable: it is where a Dependabot bump's"+
			" generated changes land once the bump has merged",
	)

	applyPatches := step(t, job.Steps, "📋 Apply patches")

	env, hasEnv := applyPatches["env"].(map[string]any)
	require.True(t, hasEnv, "📋 Apply patches must carry an `env:` mapping")

	assert.Equal(
		t,
		approvedDesktopTidyOnlyRestriction,
		env["DESKTOP_TIDY_ONLY"],
		"the Dependabot restriction must key on the pull request's author; any other"+
			" expression either leaks generated-file commits onto Dependabot branches"+
			" (#6832) or withholds the desktop tidy from everyone (#6974)",
	)

	run, hasRun := applyPatches["run"].(string)
	require.True(t, hasRun, "📋 Apply patches must carry a string `run:` script")

	assert.Contains(
		t,
		run,
		approvedDesktopTidyOnlyGuard,
		"📋 Apply patches must actually skip non-desktop patches when the restriction is"+
			" set — an unread DESKTOP_TIDY_ONLY is a restriction in name only",
	)

	assert.Contains(
		t,
		run,
		"continue",
		"the guard must skip the patch rather than only logging about it",
	)
}

// Pins the desktop tidy's delivery onto Dependabot branches.
//
// desktop/ is a separate Go module that vendors the root via `replace => ../`, so a root
// dependency bump leaves desktop/go.{mod,sum} stale. ci.yaml's verify-desktop-tidy job
// computes the fix and uploads it as desktop-tidy.patch, and desktop.yaml deliberately
// SKIPS its own hard `go mod tidy -diff` gate on same-repo PRs on the stated grounds that
// this self-heal covers them.
//
// Between 2026-09-03 and 2026-09-08 it did not: the PR-branch push excluded Dependabot,
// so on a bump the patch was computed, uploaded, applied — and then never committed, while
// the job reported success. The bump's own required checks failed on the stale module, so
// it could never merge, and the protected-branch path that was supposed to repair it only
// runs after a merge. Three bumps (#6971, #6972, #6973) stalled on one root cause, with a
// clean before/after: every bump merged before the exclusion carried an automated tidy
// commit, every one after needed a hand-pushed one.
//
// The two workflows defer to each other, so this test exists to keep at least one of them
// honest: if the self-heal is ever restricted away from Dependabot again, desktop.yaml's
// skipped gate means nothing catches it until a bump goes red.
func TestApplyPatchesDeliversDesktopTidyToDependabotBranches(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/ci.yaml")

	var workflow autoCommitWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	job, found := workflow.Jobs["auto-commit"]
	require.True(t, found, "ci workflow must define the auto-commit job")

	run, hasRun := step(t, job.Steps, "📋 Apply patches")["run"].(string)
	require.True(t, hasRun, "📋 Apply patches must carry a string `run:` script")

	// The restriction names the one patch that survives it. If verify-desktop-tidy's
	// artifact is ever renamed, the guard silently starts skipping every patch on a
	// Dependabot branch — including the desktop tidy — and the stall returns.
	assert.Contains(
		t,
		run,
		`!= "desktop-tidy.patch"`,
		"the guard must admit desktop-tidy.patch by name; a rename of the uploaded"+
			" artifact must break this test rather than silently restore the stall",
	)

	desktopTidy := readRepoFile(t, ".github/workflows/ci.yaml")
	assert.Contains(
		t,
		string(desktopTidy),
		"name: desktop-tidy-patch",
		"verify-desktop-tidy must keep uploading the artifact under the name the guard"+
			" admits; the two are only connected by this string",
	)
}

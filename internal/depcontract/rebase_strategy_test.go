package depcontract_test

import (
	"errors"
	"fmt"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	// rebaseOnRequest is the Dependabot rebase strategy that rebases a pull request only when
	// someone asks for it with `@dependabot rebase`.
	rebaseOnRequest = "disabled"

	rootGoModulesEntry = "updates:\n  - package-ecosystem: gomod\n    directory: /\n"
	rebaseOnRequestKey = "    rebase-strategy: disabled\n"
)

var errGoModulesRebaseAutomatically = errors.New(
	"go module update pull requests would be rebased on every conflicting push to main",
)

// TestGoModuleUpdatesRebaseOnRequest guards how Go module update pull requests are refreshed
// (ksail#7579).
//
// Nearly every Go module bump edits go.mod and go.sum, so merging one leaves every other open Go
// module pull request conflicting. With Dependabot's default strategy each of those is rebased
// at once and re-runs the whole CI, so one merge costs a full CI run per open pull request. That
// includes the ones that cannot merge until an upstream release lands: six of those were rebased
// 53 times between them without any becoming mergeable. Rebasing on request costs one run, for
// the pull request about to merge.
func TestGoModuleUpdatesRebaseOnRequest(t *testing.T) {
	t.Parallel()

	err := checkGoModulesRebaseOnRequest(readRepositoryFile(t, dependabotConfigPath))
	if err != nil {
		t.Fatal(err)
	}
}

// rebaseStrategyCase is one Dependabot configuration and the error checkGoModulesRebaseOnRequest
// should return for it.
type rebaseStrategyCase struct {
	name       string
	dependabot string
	wantErr    error
}

// TestCheckGoModulesRebaseOnRequestAccepts covers the configurations that rebase on request.
func TestCheckGoModulesRebaseOnRequestAccepts(t *testing.T) {
	t.Parallel()

	runRebaseStrategyCases(t, []rebaseStrategyCase{
		{
			name:       "root entry rebases on request",
			dependabot: rootGoModulesEntry + rebaseOnRequestKey,
		},
		{
			name: "root entry declared through directories rebases on request",
			dependabot: "updates:\n  - package-ecosystem: gomod\n    directories: [\"/\"]\n" +
				rebaseOnRequestKey,
		},
	})
}

// TestCheckGoModulesRebaseOnRequestRejects covers the configurations that would rebase
// automatically, and one the check cannot read.
func TestCheckGoModulesRebaseOnRequestRejects(t *testing.T) {
	t.Parallel()

	runRebaseStrategyCases(t, []rebaseStrategyCase{
		{
			// Dependabot's default is `auto`, so leaving the key out is the regression itself.
			name:       "no rebase strategy",
			dependabot: rootGoModulesEntry,
			wantErr:    errGoModulesRebaseAutomatically,
		},
		{
			name:       "automatic rebase strategy",
			dependabot: rootGoModulesEntry + "    rebase-strategy: auto\n",
			wantErr:    errGoModulesRebaseAutomatically,
		},
		{
			name: "only another ecosystem rebases on request",
			dependabot: "updates:\n  - package-ecosystem: npm\n    directory: /\n" +
				rebaseOnRequestKey + "  - package-ecosystem: gomod\n    directory: /\n",
			wantErr: errGoModulesRebaseAutomatically,
		},
		{
			name: "no gomod entry for the repository root",
			dependabot: "updates:\n  - package-ecosystem: gomod\n    directory: /tools\n" +
				rebaseOnRequestKey,
			wantErr: errGoModulesRebaseAutomatically,
		},
		{
			name:       "no updates",
			dependabot: "version: 2\n",
			wantErr:    errGoModulesRebaseAutomatically,
		},
		{
			name:       "unparseable configuration",
			dependabot: "updates: [\n",
			wantErr:    errUnreadableContractInput,
		},
	})
}

// runRebaseStrategyCases runs checkGoModulesRebaseOnRequest on each case in parallel.
func runRebaseStrategyCases(t *testing.T, testCases []rebaseStrategyCase) {
	t.Helper()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := checkGoModulesRebaseOnRequest([]byte(testCase.dependabot))
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf(
					"checkGoModulesRebaseOnRequest() error = %v, want %v",
					err, testCase.wantErr,
				)
			}
		})
	}
}

// checkGoModulesRebaseOnRequest reports whether the Dependabot entry for the root go.mod rebases
// its pull requests only when asked to.
func checkGoModulesRebaseOnRequest(contents []byte) error {
	var config struct {
		Updates []dependabotUpdate `yaml:"updates"`
	}

	err := yaml.Unmarshal(contents, &config)
	if err != nil {
		return fmt.Errorf("%w: parse %s: %w", errUnreadableContractInput, dependabotConfigPath, err)
	}

	root := rootGoModulesUpdate(config.Updates)
	if root == nil {
		return fmt.Errorf(
			"%w: %s has no gomod entry for the repository root",
			errGoModulesRebaseAutomatically, dependabotConfigPath,
		)
	}

	if root.RebaseStrategy != rebaseOnRequest {
		return fmt.Errorf(
			"%w: set rebase-strategy: %s on the root gomod entry in %s, and comment "+
				"`@dependabot rebase` on the pull request you are about to merge",
			errGoModulesRebaseAutomatically, rebaseOnRequest, dependabotConfigPath,
		)
	}

	return nil
}

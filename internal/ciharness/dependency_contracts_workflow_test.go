package ciharness_test

import (
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

const (
	dependencyContractsJob     = "verify-dependency-contracts"
	dependencyContractsFilter  = "dependency-contracts"
	dependencyContractsPackage = "internal/depcontract"
	dependencyContractsCommand = "go test ./" + dependencyContractsPackage + "/... -count=1"
)

// dependencyContractLiteral matches a quoted Go string that could be a repository path: one
// without whitespace or escapes.
var dependencyContractLiteral = regexp.MustCompile(`"([^"\s\\]+)"`)

// The contracts in internal/depcontract read files the Go build cannot see: the Dependabot
// configuration and the Cilium chart pin. The shared Go validation runs tests only for a Go
// source or module change, so a change to one of those files alone would merge with its
// contract unexecuted and redden main afterwards, which is how ksail#6750 landed. ci.yaml runs
// the package for them through a filter of its own, and this test holds that filter to every
// repository file the package names.
func TestDependencyContractsRunWhenOnlyTheirInputsChange(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	job, found := workflow.Jobs[dependencyContractsJob]
	require.True(t, found, "a change to a contract input alone needs a job of its own")
	assert.Contains(t, job.Needs, "changes")
	assert.Equal(t, "needs.changes.outputs."+dependencyContractsFilter+" == 'true'", job.If)
	assert.False(t, job.ContinueOnError)
	assert.Equal(t, "${{ steps.filter.outputs."+dependencyContractsFilter+" }}",
		workflow.Jobs["changes"].Outputs[dependencyContractsFilter])

	filter := findHarnessStep(t, workflow.Jobs["changes"].Steps, "🔍 Filter paths")

	var filters map[string][]string
	require.NoError(t, yaml.Unmarshal([]byte(stringValue(filter.With["filters"])), &filters))
	patterns := filters[dependencyContractsFilter]
	require.NotEmpty(t, patterns)

	inputs := dependencyContractInputPaths(t)
	for _, known := range []string{
		".github/dependabot.yaml", "pkg/svc/installer/cni/cilium/Dockerfile", "go.mod",
	} {
		assert.Containsf(t, inputs, known, "input discovery must find %s", known)
	}

	inputs = append(inputs, ".github/workflows/ci.yaml", "go.sum")
	for _, input := range inputs {
		assert.Truef(t, contractFilterMatches(t, patterns, input),
			"changing only %s must run the dependency contracts", input)
	}

	assert.False(t, contractFilterMatches(t, patterns, "README.md"),
		"the dependency contracts should not be selected by unrelated prose")

	aggregate := workflow.Jobs["require-checks-in-pr"]
	assert.Contains(t, aggregate.Needs, dependencyContractsJob)
	require.NotEmpty(t, aggregate.Steps)
	assert.Contains(
		t,
		aggregate.Steps[0].With["job-results"],
		"${{ needs."+dependencyContractsJob+".result }}",
	)

	step := findHarnessStep(t, job.Steps, "🔗 Verify dependency contracts")
	assert.Equal(t, dependencyContractsCommand, strings.TrimSpace(step.Run),
		"the two non-Go inputs are outside Go's test cache, so the run must not replay one")
	assert.Empty(t, step.If, "a selected contract run must not skip its test step")
	assert.False(t, step.ContinueOnError)
}

// dependencyContractInputPaths returns every repository file internal/depcontract depends on:
// its own sources, because editing one changes what the contract job asserts, and every quoted
// string in them that names a file in this repository.
func dependencyContractInputPaths(t *testing.T) []string {
	t.Helper()

	sources, err := filepath.Glob(filepath.Join("..", "..", dependencyContractsPackage, "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, sources, "input discovery must examine the contract sources")

	var paths []string

	for _, source := range sources {
		paths = append(paths, dependencyContractsPackage+"/"+filepath.Base(source))

		contents, readErr := os.ReadFile(source) //nolint:gosec // repository-owned sources.
		require.NoError(t, readErr)

		for _, match := range dependencyContractLiteral.FindAllSubmatch(contents, -1) {
			candidate := string(match[1])

			info, statErr := os.Stat(filepath.Join("..", "..", filepath.FromSlash(candidate)))
			if statErr == nil && info.Mode().IsRegular() {
				paths = append(paths, candidate)
			}
		}
	}

	slices.Sort(paths)

	return slices.Compact(paths)
}

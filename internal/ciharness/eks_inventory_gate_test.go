package ciharness_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEKSInventoryFailureReachesRequiredAggregate(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	gate := workflow.Jobs["require-checks-in-pr"]
	require.Contains(
		t,
		gate.Needs,
		"eks-smoke-scripts",
		"required checks must wait for the inventory tests",
	)
	require.NotEmpty(t, gate.Steps)
	results := stringValue(gate.Steps[0].With["job-results"])
	boundResult := regexp.MustCompile(
		`(?:^|\s)\$\{\{\s*needs\.eks-smoke-scripts\.result\s*\}\}(?:\s|$)`,
	)
	assert.True(
		t,
		boundResult.MatchString(results),
		"the aggregate must consume the inventory job's real result",
	)
	assert.False(t, workflow.Jobs["eks-smoke-scripts"].ContinueOnError)
}

func TestEKSInventoryFixtureAndWrapperChangesSelectTheirTests(t *testing.T) {
	t.Parallel()

	patterns := changesFilterPatterns(t, "eks-smoke-scripts")
	for _, path := range []string{
		"internal/maintenance/eksinventory/testdata/resources.json",
		"internal/maintenance/eksinventory/report.go",
		".github/scripts/report-eks-account-capacity.sh",
	} {
		assert.True(
			t,
			contractFilterMatches(t, patterns, path),
			"change must select inventory tests: %s",
			path,
		)
	}
}

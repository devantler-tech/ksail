package ciharness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	workflowRunCleanupMinimumMinutes = 180
	packageCleanupMinimumMinutes     = 30
)

//nolint:tagliatelle // GitHub Actions defines this external key in kebab-case.
type maintenanceWorkflow struct {
	Jobs map[string]struct {
		TimeoutMinutes int `yaml:"timeout-minutes"`
	} `yaml:"jobs"`
}

func TestMaintenanceCleanupBudgetsCoverObservedWork(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/maintenance.yaml")

	var workflow maintenanceWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	workflowRunCleanup, found := workflow.Jobs["delete-old-workflow-runs"]
	require.True(t, found, "workflow-run cleanup job is missing")
	assert.GreaterOrEqual(
		t,
		workflowRunCleanup.TimeoutMinutes,
		workflowRunCleanupMinimumMinutes,
		"workflow-run cleanup previously needed about 143 minutes",
	)

	packageCleanup, found := workflow.Jobs["delete-old-images"]
	require.True(t, found, "package cleanup job is missing")
	assert.GreaterOrEqual(
		t,
		packageCleanup.TimeoutMinutes,
		packageCleanupMinimumMinutes,
		"package cleanup has repeatedly exceeded its previous 10-minute budget",
	)
}

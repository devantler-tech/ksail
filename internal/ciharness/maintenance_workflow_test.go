package ciharness_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

//nolint:tagliatelle // GitHub Actions defines this external key in kebab-case.
type maintenanceWorkflow struct {
	On struct {
		Schedule []struct {
			Cron string `yaml:"cron"`
		} `yaml:"schedule"`
	} `yaml:"on"`
	Jobs map[string]struct {
		If             string            `yaml:"if"`
		TimeoutMinutes int               `yaml:"timeout-minutes"`
		Permissions    map[string]string `yaml:"permissions"`
		Steps          []harnessStep     `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestMaintenanceWorkflowUsesBoundedDailyRunCleanup(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/maintenance.yaml")

	var workflow maintenanceWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	crons := make([]string, 0, len(workflow.On.Schedule))
	for _, schedule := range workflow.On.Schedule {
		crons = append(crons, schedule.Cron)
	}

	assert.ElementsMatch(t, []string{"0 0 * * *", "15 0 1 * *"}, crons)

	workflowRunCleanup, found := workflow.Jobs["delete-old-workflow-runs"]
	require.True(t, found, "workflow-run cleanup job is missing")
	assert.Equal(t, 30, workflowRunCleanup.TimeoutMinutes)
	assert.Contains(t, workflowRunCleanup.If, "github.event_name == 'workflow_dispatch'")
	assert.Contains(t, workflowRunCleanup.If, "github.event.schedule == '0 0 * * *'")
	assert.Equal(t, "write", workflowRunCleanup.Permissions["actions"])
	assert.Equal(t, "read", workflowRunCleanup.Permissions["contents"])

	checkout := findHarnessStep(t, workflowRunCleanup.Steps, "📄 Checkout")
	assert.Contains(t, checkout.Uses, "actions/checkout@")
	assert.Equal(t, false, checkout.With["persist-credentials"])

	cleanup := findHarnessStep(t, workflowRunCleanup.Steps, "Delete old workflow runs")
	assert.Equal(t, "${{ github.token }}", cleanup.Env["GH_TOKEN"])
	assert.Contains(t, cleanup.Run, ".github/scripts/delete-old-workflow-runs.sh")
	assert.Contains(t, cleanup.Run, "--max-deletions 750")
	assert.Empty(t, cleanup.Uses)

	packageCleanup, found := workflow.Jobs["delete-old-images"]
	require.True(t, found, "package cleanup job is missing")
	assert.Equal(t, 60, packageCleanup.TimeoutMinutes)
	assert.Contains(t, packageCleanup.If, "github.event_name == 'workflow_dispatch'")
	assert.Contains(
		t,
		packageCleanup.If,
		"github.event.schedule == '15 0 1 * *'",
	)
}

func TestWorkflowRunCleanupScript(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")

	command := exec.CommandContext(
		t.Context(),
		"bash",
		"../../.github/scripts/delete-old-workflow-runs.test.sh",
	)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "workflow-run cleanup contract failed:\n%s", output)
	assert.Contains(t, string(output), "PASS: cleanup preserves protected runs")
	assert.Contains(t, string(output), "PASS: cleanup fails closed")
}

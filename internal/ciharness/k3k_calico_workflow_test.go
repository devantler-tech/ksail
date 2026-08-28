package ciharness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	defaultBranchGHCRToken = "${{ github.event_name != 'pull_request' && " +
		"github.ref_name == github.event.repository.default_branch && secrets.GITHUB_TOKEN || '' }}"
	defaultBranchDockerHubToken = "${{ github.event_name != 'pull_request' && " +
		"github.ref_name == github.event.repository.default_branch && secrets.DOCKERHUB_TOKEN || '' }}"
)

//nolint:tagliatelle // GitHub Actions defines these external keys in kebab-case.
type k3kCalicoWorkflow struct {
	On struct {
		WorkflowDispatch map[string]any `yaml:"workflow_dispatch"`
		PullRequest      struct {
			Paths []string `yaml:"paths"`
		} `yaml:"pull_request"`
		Schedule []struct {
			Cron string `yaml:"cron"`
		} `yaml:"schedule"`
	} `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		TimeoutMinutes int               `yaml:"timeout-minutes"`
		Permissions    map[string]string `yaml:"permissions"`
		Strategy       any               `yaml:"strategy"`
		Steps          []harnessStep     `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestK3KCalicoSentinelRunsWeeklyOutsidePullRequests(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-k3k-calico.yaml")

	var workflow k3kCalicoWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	require.Len(t, workflow.On.Schedule, 1, "sentinel must have one weekly schedule")
	assert.Equal(t, "30 2 * * 1", workflow.On.Schedule[0].Cron)
	assert.NotNil(t, workflow.On.WorkflowDispatch, "sentinel must remain manually dispatchable")
	assert.Contains(
		t,
		workflow.On.PullRequest.Paths,
		".github/workflows/system-test-k3k-calico.yaml",
		"workflow changes must exercise the real sentinel before merge",
	)
	assert.Equal(t, "read", workflow.Permissions["contents"])

	require.Len(t, workflow.Jobs, 1, "scheduled sentinel must remain one bounded test leg")
	job, found := workflow.Jobs["k3k-calico-sentinel"]
	require.True(t, found, "k3k+Calico sentinel job is missing")
	assert.Equal(t, 120, job.TimeoutMinutes)
	assert.Nil(t, job.Strategy, "sentinel must not expand into a system-test matrix")
	assert.Equal(t, "read", job.Permissions["contents"])
	assert.Equal(t, "read", job.Permissions["packages"])

	checkout := findHarnessStep(t, job.Steps, "📄 Checkout")
	assert.Equal(t, false, checkout.With["persist-credentials"])

	assertDockerHubLoginGuard(t, job.Steps)

	systemTest := findHarnessStep(t, job.Steps, "🧪 Run k3k + Calico Sentinel")
	assert.Equal(t, "./.github/actions/ksail-system-test", systemTest.Uses)
	assert.Equal(t, "Vanilla", systemTest.With["distribution"])
	assert.Equal(t, "Docker", systemTest.With["provider"])
	assert.Equal(t, "true", systemTest.With["init"])
	assert.Equal(t, "--name k3k-calico-host", systemTest.With["args"])
	assert.Equal(t, "true", systemTest.With["test-kubernetes-provider"])
	assert.Equal(t, "K3s", systemTest.With["kubernetes-provider-distributions"])
	assert.Equal(t, "Calico", systemTest.With["kubernetes-provider-cni"])
	assert.Equal(
		t,
		defaultBranchGHCRToken,
		systemTest.With["ghcr-token"],
	)
	assert.Equal(
		t,
		defaultBranchDockerHubToken,
		systemTest.With["dockerhub-token"],
	)
	assert.Equal(t, "false", systemTest.With["cleanup"])
	assert.Equal(t, "false", systemTest.With["upload-artifacts"])

	cleanup := findHarnessStep(t, job.Steps, "🧪 Cleanup KSail System Test")
	assert.Equal(t, "./.github/actions/ksail-system-test-cleanup", cleanup.Uses)
	assert.Contains(t, cleanup.If, "always()")
}

func assertDockerHubLoginGuard(t *testing.T, steps []harnessStep) {
	t.Helper()

	dockerHubLogin := findHarnessStep(t, steps, "🔐 Login to Docker Hub")
	assert.Equal(t, "${{ secrets.DOCKERHUB_TOKEN }}", dockerHubLogin.Env["DOCKERHUB_TOKEN"])
	assert.Contains(t, dockerHubLogin.If, "github.event_name != 'pull_request'")
	assert.Contains(
		t,
		dockerHubLogin.If,
		"github.ref_name == github.event.repository.default_branch",
	)
	assert.Contains(t, dockerHubLogin.If, "vars.DOCKERHUB_USERNAME != ''")
	assert.Contains(t, dockerHubLogin.If, "env.DOCKERHUB_TOKEN != ''")
	assert.Equal(t, "${{ env.DOCKERHUB_TOKEN }}", dockerHubLogin.With["password"])
}

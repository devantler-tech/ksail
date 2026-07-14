package ciharness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

//nolint:tagliatelle // GitHub Actions defines these external keys in kebab-case.
type harnessStep struct {
	Name            string            `yaml:"name"`
	ID              string            `yaml:"id"`
	If              string            `yaml:"if"`
	Env             map[string]string `yaml:"env"`
	Uses            string            `yaml:"uses"`
	Run             string            `yaml:"run"`
	TimeoutMinutes  int               `yaml:"timeout-minutes"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	With            map[string]any    `yaml:"with"`
}

type compositeAction struct {
	Inputs map[string]struct {
		Default string `yaml:"default"`
	} `yaml:"inputs"`
	Outputs map[string]struct {
		Value string `yaml:"value"`
	} `yaml:"outputs"`
	Runs struct {
		Steps []harnessStep `yaml:"steps"`
	} `yaml:"runs"`
}

type ciWorkflow struct {
	Jobs map[string]struct {
		Steps []harnessStep `yaml:"steps"`
	} `yaml:"jobs"`
}

//nolint:funlen // One test locks the cross-file action/workflow contract end to end.
func TestSystemTestHarnessBoundsReservedSandboxRecovery(t *testing.T) {
	t.Parallel()

	clusterAction := readCompositeAction(t, ".github/actions/ksail-cluster/action.yml")
	createStep := findHarnessStep(t, clusterAction.Runs.Steps, "🚀 Create Cluster")

	assert.Contains(t, createStep.Run, `MAX_ATTEMPTS=2`)
	assert.Contains(t, createStep.Run, `repeated reserved pod sandbox failures`)
	assert.Contains(t, createStep.Run, `should_retry_create "$LOG_FILE"`)
	assert.Contains(t, createStep.Run, `timeout --kill-after=10s`)

	systemAction := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	require.Contains(t, systemAction.Inputs, "upload-artifacts")
	assert.Equal(t, "true", systemAction.Inputs["upload-artifacts"].Default)
	require.Contains(t, systemAction.Inputs, "comprehensive-debug")
	assert.Equal(t, "true", systemAction.Inputs["comprehensive-debug"].Default)
	require.Contains(t, systemAction.Inputs, "cleanup")
	assert.Equal(t, "true", systemAction.Inputs["cleanup"].Default)
	require.Contains(t, systemAction.Outputs, "artifact-tag")
	assert.Equal(
		t,
		"${{ steps.generate-tag.outputs.tag }}",
		systemAction.Outputs["artifact-tag"].Value,
	)

	diagnosticUpload := findHarnessStep(
		t,
		systemAction.Runs.Steps,
		"📤 Upload system test diagnostics",
	)
	logUpload := findHarnessStep(t, systemAction.Runs.Steps, "📤 Upload system test logs")
	assert.Contains(t, diagnosticUpload.If, "inputs.upload-artifacts == 'true'")
	assert.Contains(t, logUpload.If, "inputs.upload-artifacts == 'true'")
	debugStep := findHarnessStep(t, systemAction.Runs.Steps, "🐞 Debug Kubernetes failure")
	assert.Contains(t, debugStep.If, "inputs.comprehensive-debug == 'true'")

	cleanupStep := findHarnessStep(
		t,
		systemAction.Runs.Steps,
		"🧪 Cleanup KSail System Test",
	)
	assert.Equal(t, "./.github/actions/ksail-system-test-cleanup", cleanupStep.Uses)
	assert.Contains(t, cleanupStep.If, "inputs.cleanup == 'true'")
	assert.Equal(t, "${{ inputs.args }}", stringValue(cleanupStep.With["args"]))

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	dockerJob, ok := workflow.Jobs["system-test-docker"]
	require.True(t, ok, "system-test-docker job is missing")
	runStep := findHarnessStep(t, dockerJob.Steps, "🧪 Run KSail System Test")
	assert.Equal(t, "system-test", runStep.ID)
	assert.Equal(t, "false", stringValue(runStep.With["upload-artifacts"]))
	assert.Equal(t, "false", stringValue(runStep.With["cleanup"]))
	assert.Contains(
		t,
		stringValue(runStep.With["comprehensive-debug"]),
		"matrix.distribution == 'K3s'",
	)

	assertBoundedWorkflowUpload(
		t,
		dockerJob.Steps,
		"📤 Upload system test diagnostics",
		"/tmp/system-test-diagnostics/",
		"failure()",
	)
	workflowCleanup := findHarnessStep(
		t,
		dockerJob.Steps,
		"🧪 Cleanup KSail System Test",
	)
	assert.Equal(t, "./.github/actions/ksail-system-test-cleanup", workflowCleanup.Uses)
	assert.Contains(t, workflowCleanup.If, "always()")
	assert.Equal(t, "${{ matrix.distribution }}", stringValue(workflowCleanup.With["distribution"]))
	assert.Equal(t, "${{ matrix.provider }}", stringValue(workflowCleanup.With["provider"]))
	assert.Equal(t, "${{ matrix.args }}", stringValue(workflowCleanup.With["args"]))

	diagnosticUploadIndex := findHarnessStepIndex(
		t,
		dockerJob.Steps,
		"📤 Upload system test diagnostics",
	)
	cleanupIndex := findHarnessStepIndex(t, dockerJob.Steps, "🧪 Cleanup KSail System Test")
	logUploadIndex := findHarnessStepIndex(t, dockerJob.Steps, "📤 Upload system test logs")
	assert.Less(t, diagnosticUploadIndex, cleanupIndex)
	assert.Less(t, cleanupIndex, logUploadIndex)
	assertBoundedWorkflowUpload(
		t,
		dockerJob.Steps,
		"📤 Upload system test logs",
		"/tmp/ksail-system-test-logs/",
		"always()",
	)
}

func TestSystemTestHarnessOnlyBoundsDockerK3sCleanup(t *testing.T) {
	t.Parallel()

	cleanupAction := readCompositeAction(
		t,
		".github/actions/ksail-system-test-cleanup/action.yaml",
	)
	deleteStep := findHarnessStep(t, cleanupAction.Runs.Steps, "🧪 ksail cluster delete")
	assert.Equal(t, "${{ inputs.distribution }}", deleteStep.Env["DISTRIBUTION"])

	providerGate := strings.Index(
		deleteStep.Run,
		`if [ "$PROVIDER" = "Docker" ] && [ "$DISTRIBUTION" = "K3s" ]; then`,
	)
	require.NotEqual(
		t,
		-1,
		providerGate,
		"only Docker/K3s cleanup may use the short recovery bound",
	)

	dockerBranchOffset := strings.Index(
		deleteStep.Run[providerGate:],
		"\nelif [ \"$PROVIDER\" = \"Docker\" ]; then\n",
	)
	require.NotEqual(
		t,
		-1,
		dockerBranchOffset,
		"cluster cleanup must preserve a non-K3s Docker branch",
	)

	cloudBranchOffset := strings.Index(
		deleteStep.Run[providerGate+dockerBranchOffset:],
		"\nelse\n",
	)
	require.NotEqual(t, -1, cloudBranchOffset, "cluster cleanup must include a cloud branch")

	dockerBranchStart := providerGate + dockerBranchOffset
	cloudBranchStart := dockerBranchStart + cloudBranchOffset
	k3sBranch := deleteStep.Run[providerGate:dockerBranchStart]
	dockerBranch := deleteStep.Run[dockerBranchStart:cloudBranchStart]
	cloudBranch := deleteStep.Run[cloudBranchStart:]

	assert.Contains(
		t,
		k3sBranch,
		`timeout --kill-after=10s 2m`,
	)
	assert.Contains(t, k3sBranch, `|| echo`)
	assert.NotContains(t, dockerBranch, `timeout --kill-after=10s 2m`)
	assert.Contains(t, dockerBranch, `"${DELETE_COMMAND[@]}"`)
	assert.Contains(t, dockerBranch, `|| echo`)
	assert.NotContains(t, cloudBranch, `timeout --kill-after=10s 2m`)
	assert.Contains(t, cloudBranch, `"${DELETE_COMMAND[@]}"`)
	assert.NotContains(t, cloudBranch, `|| echo`)
}

func readCompositeAction(t *testing.T, path string) compositeAction {
	t.Helper()

	contents := readRepoFile(t, path)

	var action compositeAction
	require.NoError(t, yaml.Unmarshal(contents, &action))

	return action
}

func readCIWorkflow(t *testing.T, path string) ciWorkflow {
	t.Helper()

	contents := readRepoFile(t, path)

	var workflow ciWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	return workflow
}

func readRepoFile(t *testing.T, path string) []byte {
	t.Helper()

	// The caller supplies repository-owned fixture paths, never user input.
	contents, err := os.ReadFile(filepath.Join("..", "..", path)) //nolint:gosec
	require.NoError(t, err)

	return contents
}

func findHarnessStep(t *testing.T, steps []harnessStep, name string) harnessStep {
	t.Helper()

	for _, step := range steps {
		if step.Name == name {
			return step
		}
	}

	t.Fatalf("step %q is missing", name)

	return harnessStep{}
}

func findHarnessStepIndex(t *testing.T, steps []harnessStep, name string) int {
	t.Helper()

	for index, step := range steps {
		if step.Name == name {
			return index
		}
	}

	t.Fatalf("step %q is missing", name)

	return -1
}

func assertBoundedWorkflowUpload(
	t *testing.T,
	steps []harnessStep,
	name string,
	wantPath string,
	wantCondition string,
) {
	t.Helper()

	step := findHarnessStep(t, steps, name)
	assert.True(t, strings.HasPrefix(step.Uses, "actions/upload-artifact@"))
	assert.Equal(t, 5, step.TimeoutMinutes)
	assert.True(t, step.ContinueOnError)
	assert.Contains(t, step.If, wantCondition)
	assert.Equal(t, wantPath, stringValue(step.With["path"]))
	assert.Contains(t, stringValue(step.With["name"]), "steps.system-test.outputs.artifact-tag")
}

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(text)
}

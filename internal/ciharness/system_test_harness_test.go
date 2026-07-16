package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	eksSmokeBoundaryFixture = "arn:aws-us-gov:iam::123456789012:policy/eks-ci-smoke-boundary"
	eksSmokeConfigFixture   = `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: fixture
  region: us-east-1
  tags:
    owner: keep
iam:
  withOIDC: true
managedNodeGroups:
  - name: primary
    desiredCapacity: 3
    minSize: 2
    maxSize: 4
    labels:
      workload: keep
    iam:
      withAddonPolicies:
        autoScaler: true
  - name: secondary
    desiredCapacity: 2
    minSize: 1
    maxSize: 3
addons:
  - name: vpc-cni
    version: latest
  - name: aws-ebs-csi-driver
    attachPolicyARNs:
      - arn:aws-us-gov:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy
  - name: kube-proxy
    version: v1.31.0-eksbuild.1
`
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

func TestEKSSmokeConfigBoundsEveryCreatedIAMRole(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	smokeJob, ok := workflow.Jobs["smoke-test"]
	require.True(t, ok, "smoke-test job is missing")

	boundaryStep := findHarnessStep(
		t,
		smokeJob.Steps,
		"🔐 Resolve EKS permissions boundary",
	)
	assert.Equal(t, "permissions-boundary", boundaryStep.ID)
	assert.Contains(t, boundaryStep.Run, `aws sts get-caller-identity`)
	assert.Contains(t, boundaryStep.Run, `--query Arn`)
	assert.Contains(t, boundaryStep.Run, `^[0-9]{12}$`)
	assert.Contains(t, boundaryStep.Run, `^aws(-[a-z0-9-]+)?$`)
	assert.Contains(t, boundaryStep.Run, `"$arn_account_id" != "$account_id"`)
	assert.Contains(t, boundaryStep.Run, `policy/eks-ci-smoke-boundary`)
	assert.Contains(t, boundaryStep.Run, `arn=arn:%s:iam::%s:policy`)
	assert.Contains(t, boundaryStep.Run, `>> "$GITHUB_OUTPUT"`)

	initStep := findHarnessStep(t, smokeJob.Steps, "🔧 Initialize EKS project")
	assert.Equal(
		t,
		"${{ steps.permissions-boundary.outputs.arn }}",
		initStep.Env["AWS_PERMISSIONS_BOUNDARY_ARN"],
	)
	assert.Contains(
		t,
		initStep.Run,
		`boundary = ENV.fetch("AWS_PERMISSIONS_BOUNDARY_ARN")`,
	)
	assert.Contains(
		t,
		initStep.Run,
		`data.fetch("iam")["serviceRolePermissionsBoundary"] = boundary`,
	)
	assert.Contains(t, initStep.Run, `nodegroup["iam"] ||= {}`)
	assert.Contains(
		t,
		initStep.Run,
		`nodegroup["iam"]["instanceRolePermissionsBoundary"] = boundary`,
	)
	assert.Contains(t, initStep.Run, `%w[vpc-cni aws-ebs-csi-driver]`)
	assert.Contains(t, initStep.Run, `addon["permissionsBoundary"] = boundary`)
}

func TestEKSSmokeConfigBoundaryMutationSemantics(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")
	requireTestExecutable(t, "ruby")

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	smokeJob, ok := workflow.Jobs["smoke-test"]
	require.True(t, ok, "smoke-test job is missing")

	boundaryStep := findHarnessStep(t, smokeJob.Steps, "🔐 Resolve EKS permissions boundary")
	assert.Equal(
		t,
		"arn="+eksSmokeBoundaryFixture+"\n",
		runPermissionsBoundaryStep(t, boundaryStep.Run),
	)

	initStep := findHarnessStep(t, smokeJob.Steps, "🔧 Initialize EKS project")
	config := runEmbeddedEKSSmokeMutation(t, initStep.Run)
	assertEKSSmokeMutation(t, config)
}

func TestEKSSmokeConfigBoundaryRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	smokeJob, ok := workflow.Jobs["smoke-test"]
	require.True(t, ok, "smoke-test job is missing")
	boundaryStep := findHarnessStep(t, smokeJob.Steps, "🔐 Resolve EKS permissions boundary")

	testCases := map[string]struct {
		accountID string
		callerARN string
	}{
		"invalid account ID": {
			accountID: "not-an-account",
			callerARN: "arn:aws:sts::123456789012:assumed-role/eks-ci/smoke-test",
		},
		"malformed caller ARN": {
			accountID: "123456789012",
			callerARN: "not-an-arn",
		},
		"foreign ARN partition": {
			accountID: "123456789012",
			callerARN: "arn:azure:sts::123456789012:assumed-role/eks-ci/smoke-test",
		},
		"mismatched ARN account": {
			accountID: "123456789012",
			callerARN: "arn:aws:sts::210987654321:assumed-role/eks-ci/smoke-test",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			boundaryOutput, diagnostics, err := executePermissionsBoundaryStep(
				t,
				boundaryStep.Run,
				testCase.accountID,
				testCase.callerARN,
			)

			require.Error(t, err)
			assert.Empty(t, boundaryOutput)
			assert.Contains(
				t,
				diagnostics,
				"AWS identity returned an invalid account ID or caller ARN",
			)
		})
	}
}

func requireTestExecutable(t *testing.T, name string) {
	t.Helper()

	_, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is unavailable: %v", name, err)
	}
}

func runPermissionsBoundaryStep(t *testing.T, workflowRun string) string {
	t.Helper()

	boundaryOutput, diagnostics, err := executePermissionsBoundaryStep(
		t,
		workflowRun,
		"123456789012",
		"arn:aws-us-gov:sts::123456789012:assumed-role/eks-ci/smoke-test",
	)
	require.NoErrorf(t, err, "permissions-boundary step failed:\n%s", diagnostics)

	return boundaryOutput
}

func executePermissionsBoundaryStep(
	t *testing.T,
	workflowRun string,
	accountID string,
	callerARN string,
) (string, string, error) {
	t.Helper()

	const awsStub = `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  *"--query Account"*)
    printf '%s\n' "$STUB_ACCOUNT_ID"
    ;;
  *"--query Arn"*)
    printf '%s\n' "$STUB_CALLER_ARN"
    ;;
  *)
    exit 64
    ;;
esac
`

	tempDir := t.TempDir()
	awsPath := filepath.Join(tempDir, "aws")
	outputPath := filepath.Join(tempDir, "github-output")

	require.NoError(
		t,
		os.WriteFile(awsPath, []byte(awsStub), 0o700), //nolint:gosec // Test-owned path.
	)
	require.NoError(
		t,
		os.WriteFile(outputPath, nil, 0o600),
	)

	// The executable and shell source are both repository-owned test inputs.
	command := exec.CommandContext(t.Context(), "bash", "-c", workflowRun) //nolint:gosec

	command.Env = append(
		os.Environ(),
		"PATH="+tempDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_OUTPUT="+outputPath,
		"STUB_ACCOUNT_ID="+accountID,
		"STUB_CALLER_ARN="+callerARN,
	)
	diagnostics, commandErr := command.CombinedOutput()

	boundaryOutput, err := os.ReadFile(outputPath) //nolint:gosec // Test-owned path.
	require.NoError(t, err)

	return string(boundaryOutput), string(diagnostics), commandErr
}

func assertEKSSmokeMutation(t *testing.T, config map[string]any) {
	t.Helper()

	metadata := requireStringMap(t, config["metadata"])
	assert.Equal(t, "fixture", metadata["name"])
	assert.Equal(t, "us-gov-west-1", metadata["region"])
	assert.Equal(t, "keep", requireStringMap(t, metadata["tags"])["owner"])

	iam := requireStringMap(t, config["iam"])
	assert.Equal(t, true, iam["withOIDC"])
	assert.Equal(t, eksSmokeBoundaryFixture, iam["serviceRolePermissionsBoundary"])

	assertEKSSmokeNodegroups(t, config["managedNodeGroups"])
	assertEKSSmokeAddons(t, config["addons"])
}

func assertEKSSmokeNodegroups(t *testing.T, rawNodegroups any) {
	t.Helper()

	nodegroups := requireAnySlice(t, rawNodegroups)
	require.Len(t, nodegroups, 2)

	for _, rawNodegroup := range nodegroups {
		nodegroup := requireStringMap(t, rawNodegroup)
		assert.Equal(t, 1, nodegroup["desiredCapacity"])
		assert.Equal(t, 1, nodegroup["minSize"])
		assert.Equal(t, 1, nodegroup["maxSize"])
		assert.Equal(
			t,
			eksSmokeBoundaryFixture,
			requireStringMap(t, nodegroup["iam"])["instanceRolePermissionsBoundary"],
		)
	}

	primary := requireStringMap(t, nodegroups[0])
	assert.Equal(t, "keep", requireStringMap(t, primary["labels"])["workload"])
	addonPolicies := requireStringMap(
		t,
		requireStringMap(t, primary["iam"])["withAddonPolicies"],
	)
	assert.Equal(t, true, addonPolicies["autoScaler"])
}

func assertEKSSmokeAddons(t *testing.T, rawAddons any) {
	t.Helper()

	addons := requireAnySlice(t, rawAddons)
	require.Len(t, addons, 3)

	for _, rawAddon := range addons[:2] {
		addon := requireStringMap(t, rawAddon)
		assert.Equal(t, eksSmokeBoundaryFixture, addon["permissionsBoundary"])
	}

	kubeProxy := requireStringMap(t, addons[2])
	assert.Equal(t, "kube-proxy", kubeProxy["name"])
	assert.Equal(t, "v1.31.0-eksbuild.1", kubeProxy["version"])
	assert.NotContains(t, kubeProxy, "permissionsBoundary")
}

func runEmbeddedEKSSmokeMutation(t *testing.T, workflowRun string) map[string]any {
	t.Helper()

	const rubyPrefix = "ruby -ryaml -e '\n"

	start := strings.Index(workflowRun, rubyPrefix)
	require.NotEqual(t, -1, start, "embedded Ruby mutation is missing")
	remainder := workflowRun[start+len(rubyPrefix):]
	end := strings.Index(remainder, "\n'")
	require.NotEqual(t, -1, end, "embedded Ruby mutation is unterminated")
	rubyScript := remainder[:end]

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "eks.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(eksSmokeConfigFixture), 0o600))

	// The command and script both come from this repository's owned workflow.
	command := exec.CommandContext( //nolint:gosec
		t.Context(), "ruby", "-ryaml", "-e", rubyScript,
	)
	command.Dir = tempDir

	command.Env = append(
		os.Environ(),
		"AWS_REGION=us-gov-west-1",
		"AWS_PERMISSIONS_BOUNDARY_ARN="+eksSmokeBoundaryFixture,
	)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "embedded Ruby mutation failed:\n%s", output)

	mutated, err := os.ReadFile(configPath) //nolint:gosec // Test-owned temporary path.
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, yaml.Unmarshal(mutated, &config))

	return config
}

func requireStringMap(t *testing.T, value any) map[string]any {
	t.Helper()

	result, ok := value.(map[string]any)
	require.True(t, ok, "expected map, got %T", value)

	return result
}

func requireAnySlice(t *testing.T, value any) []any {
	t.Helper()

	result, ok := value.([]any)
	require.True(t, ok, "expected slice, got %T", value)

	return result
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

package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A removed admission step must fail before a billable create is reachable.
func TestEKSSmokeRequiresCapacityAdmissionBeforeCreate(t *testing.T) {
	t.Parallel()
	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	steps := workflow.Jobs["smoke-test"].Steps
	gate := harnessStepIndex(t, steps, "🔎 Check EKS capacity before create")
	create := harnessStepIndex(t, steps, "🧪 ksail cluster create")
	require.Equal(t, create-1, gate)
	require.Empty(t, steps[gate].If, "capacity admission cannot be bypassed")
	require.False(t, steps[gate].ContinueOnError)
	require.Contains(t, steps[gate].Run, "check-eks-capacity.sh")
}

func TestEKSCapacityRefusalCannotReachCreate(t *testing.T) {
	t.Parallel()
	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	steps := workflow.Jobs["smoke-test"].Steps
	gate := findHarnessStep(t, steps, "🔎 Check EKS capacity before create")
	create := findHarnessStep(t, steps, "🧪 ksail cluster create")
	require.Equal(t, "2", workflow.Env["KSAIL_EKS_SMOKE_PEAK_NODES"])

	for _, mode := range []string{"admitted", "refused", "unknown", "yaml-read-failed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			scripts := filepath.Join(dir, ".github", "scripts")
			require.NoError(t, os.MkdirAll(scripts, 0o700))
			writeExecutableStub(
				t,
				filepath.Join(dir, "yq"),
				"#!/usr/bin/env bash\n[[ \"$MODE\" != yaml-read-failed ]] || exit 1\nprintf '%s\\n' '{\"plan\":true}'\n",
			)
			writeExecutableStub(
				t,
				filepath.Join(scripts, "check-eks-capacity.sh"),
				`#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == '--region eu-central-1 --cluster-name fixture --peak-nodes 2' ]] || exit 99
read -r plan
[[ "$plan" == '{"plan":true}' ]] || exit 99
[[ "$MODE" == admitted ]]
`,
			)
			writeExecutableStub(
				t,
				filepath.Join(dir, "ksail"),
				"#!/usr/bin/env bash\nprintf 'created\\n' >> \"$MARKER\"\n",
			)
			outputPath := filepath.Join(dir, "outputs")
			marker := filepath.Join(dir, "created")
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Stdin = strings.NewReader(gate.Run + "\n" + create.Run)

			command.Env = append(
				os.Environ(),
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GITHUB_WORKSPACE="+dir,
				"KSAIL_EKS_WORKDIR="+dir,
				"GITHUB_OUTPUT="+outputPath,
				"KSAIL_EKS_CLUSTER_NAME=fixture",
				"AWS_REGION=eu-central-1",
				"MODE="+mode,
				"MARKER="+marker,
				"KSAIL_EKS_SMOKE_PEAK_NODES="+workflow.Env["KSAIL_EKS_SMOKE_PEAK_NODES"],
			)

			output, err := command.CombinedOutput()
			assertCapacityCreation(t, dir, mode, outputPath, marker, output, err)
		})
	}
}

func TestEKSReadonlyCapacityProbeEmitsExplicitCandidate(t *testing.T) {
	t.Parallel()
	workflow := readCIWorkflow(t, ".github/workflows/eks-account-capacity.yaml")
	step := findHarnessStep(
		t,
		workflow.Jobs["report"].Steps,
		"🔎 Check smoke candidate quota without creating resources",
	)
	require.Equal(t, "inputs.check_smoke_capacity", step.If)
	// Execute the published probe JSON producer and capture its arguments and plan.
	dir := t.TempDir()
	scripts := filepath.Join(dir, ".github", "scripts")
	require.NoError(t, os.MkdirAll(scripts, 0o700))
	writeExecutableStub(t, filepath.Join(scripts, "check-eks-capacity.sh"), `#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == '--region eu-central-1 --cluster-name capacity-probe --peak-nodes 2' ]] || exit 99
jq -e '
.metadata.region == "eu-central-1" and
(.managedNodeGroups | length == 1) and
.managedNodeGroups[0].instanceType == "t3.medium" and
.managedNodeGroups[0].desiredCapacity == 1 and
.managedNodeGroups[0].minSize == 1 and
.managedNodeGroups[0].maxSize == 1
' > /dev/null
`)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(step.Run)

	command.Env = append(os.Environ(), "GITHUB_WORKSPACE="+dir, "AWS_REGION=eu-central-1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "candidate: %s", strings.TrimSpace(string(output)))
}

func assertCapacityCreation(
	t *testing.T,
	dir, mode, outputPath, marker string,
	output []byte,
	err error,
) {
	t.Helper()

	if mode == "admitted" {
		require.NoError(t, err, string(output))

		root, readErr := os.OpenRoot(dir)
		require.NoError(t, readErr)

		t.Cleanup(func() { require.NoError(t, root.Close()) })

		data, readErr := root.ReadFile("created")
		require.NoError(t, readErr)
		require.Contains(t, string(data), "created")
	} else {
		require.Error(t, err)

		_, readErr := os.Stat(marker)
		require.True(t, os.IsNotExist(readErr))
		_, readErr = os.Stat(outputPath)
		require.True(t, os.IsNotExist(readErr), "refusal must not set create-attempted")
	}
}

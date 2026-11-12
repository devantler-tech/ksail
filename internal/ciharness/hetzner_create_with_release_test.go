package ciharness_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const releaseCreatePrepareStep = "🔍 Prepare release creation binary"

// ksail#8347: a Hetzner dispatch can create the cluster with a published release, so updates
// are tested against a cluster an earlier release made. The option is off by default.
func TestHetznerCreateWithReleaseIsOptInAndVerified(t *testing.T) {
	t.Parallel()

	// Only workflow_dispatch is decoded: the schedule trigger is a list.
	//
	//nolint:tagliatelle // GitHub Actions defines this external key in snake_case.
	var trigger struct {
		On struct {
			WorkflowDispatch struct {
				Inputs map[string]struct {
					Type    string `yaml:"type"`
					Default any    `yaml:"default"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
		} `yaml:"on"`
	}
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &trigger),
	)

	input := trigger.On.WorkflowDispatch.Inputs["create_with_release"]
	require.Equal(t, "string", input.Type)
	require.Empty(t, input.Default)

	workflow := readCIWorkflow(t, ".github/workflows/system-test-hetzner.yaml")
	steps := workflow.Jobs["system-test"].Steps
	download := findHarnessStep(t, steps, "📥 Download the KSail release that creates the cluster")
	assert.Contains(t, download.If, "matrix.smoke != true")
	assert.Contains(t, download.If, "github.event_name == 'workflow_dispatch'")
	assert.Contains(t, download.If, "inputs.create_with_release != ''")
	assert.Positive(t, download.TimeoutMinutes)
	assert.Contains(
		t,
		download.Run,
		`^v([0-9]+\.[0-9]+\.[0-9]+)$`,
		"only a release tag is accepted",
	)
	assert.Contains(t, download.Run, "_checksums.txt", "the archive is checked against the release")
	assert.Contains(t, download.Run, "sha256sum --check --strict")

	system := findHarnessStep(t, steps, "🧪 Run KSail System Test")
	assert.Equal(
		t,
		"${{ steps.create-release.outputs.binary }}",
		stringValue(system.With["create-binary"]),
	)
	assert.Equal(
		t,
		"${{ steps.create-release.outputs.sha256 }}",
		stringValue(system.With["create-binary-sha256"]),
	)
	assert.Less(
		t,
		harnessStepIndex(t, steps, download.Name),
		harnessStepIndex(t, steps, system.Name),
	)
}

// The release binary only runs project init and cluster create; every later step uses the
// build under test.
func TestReleaseCreationBinaryOnlyCreatesTheCluster(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	require.Contains(t, action.Inputs, "create-binary")
	assert.Empty(t, action.Inputs["create-binary"].Default)

	prepare := findHarnessStep(t, action.Runs.Steps, releaseCreatePrepareStep)
	create := findHarnessStep(t, action.Runs.Steps, "🧪 ksail cluster create")
	assert.Equal(t, "inputs.create-binary != ''", prepare.If)
	assert.NotContains(
		t,
		prepare.Run,
		"GITHUB_PATH",
		"the release must not select the CLI used after creation",
	)
	assert.Contains(t, create.Env["PATH"], "steps.release-create-path.outputs.path")
	assert.Less(
		t,
		harnessStepIndex(t, action.Runs.Steps, prepare.Name),
		harnessStepIndex(t, action.Runs.Steps, create.Name),
	)

	for _, step := range action.Runs.Steps[harnessStepIndex(t, action.Runs.Steps, create.Name)+1:] {
		assert.NotContains(
			t,
			step.Env["PATH"],
			"release-create-path",
			"%s must use the build under test",
			step.Name,
		)
	}
}

type releasePrepareCase struct {
	name         string
	distribution string
	calico       bool
	badChecksum  bool
	wantErr      string
}

// The prepare step refuses every input it cannot vouch for, and otherwise puts only the
// verified release binary ahead of the build under test.
func TestReleaseCreationBinaryPrepareGuards(t *testing.T) {
	t.Parallel()

	prepare := findHarnessStep(
		t,
		readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml").Runs.Steps,
		releaseCreatePrepareStep,
	)

	for _, testCase := range []releasePrepareCase{
		{name: "verified Talos release", distribution: "Talos"},
		{name: "checksum mismatch", distribution: "Talos", badChecksum: true, wantErr: "differs"},
		{name: "with the Calico migration binary", distribution: "Talos", calico: true, wantErr: "Calico"},
		{name: "not Talos", distribution: "K3s", wantErr: "Talos"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runReleasePrepare(t, prepare.Run, testCase)
		})
	}
}

func runReleasePrepare(t *testing.T, script string, testCase releasePrepareCase) {
	t.Helper()

	dir := t.TempDir()
	releaseDir := filepath.Join(dir, "release")
	candidateDir := filepath.Join(dir, "candidate")

	require.NoError(t, os.MkdirAll(releaseDir, 0o750))
	require.NoError(t, os.MkdirAll(candidateDir, 0o750))

	release := writeFakeKSail(t, releaseDir, "ksail version 7.212.22")
	writeFakeKSail(t, candidateDir, "ksail version candidate")

	content, err := os.ReadFile(release) //nolint:gosec // a file this test just wrote
	require.NoError(t, err)

	digest := sha256.Sum256(content)

	checksum := hex.EncodeToString(digest[:])
	if testCase.badChecksum {
		checksum = strings.Repeat("0", len(checksum))
	}

	output := filepath.Join(dir, "github-output")
	require.NoError(t, os.WriteFile(output, nil, 0o600))

	calico := ""
	if testCase.calico {
		calico = release
	}

	//nolint:gosec // runs the reviewed workflow step under test
	command := exec.CommandContext(t.Context(), "bash", "-c", script)
	command.Env = []string{
		"PATH=" + candidateDir + ":/usr/bin:/bin",
		"GITHUB_OUTPUT=" + output,
		"CREATE_BINARY=" + release,
		"CREATE_SHA256=" + checksum,
		"CALICO_BASELINE=" + calico,
		"DISTRIBUTION=" + testCase.distribution,
	}
	log, runErr := command.CombinedOutput()

	written, err := os.ReadFile(output) //nolint:gosec // a file this test created
	require.NoError(t, err)

	if testCase.wantErr != "" {
		require.Error(t, runErr, string(log))
		assert.Contains(t, string(log), testCase.wantErr)
		assert.Empty(t, written, "a refused binary must not reach the create step")

		return
	}

	require.NoError(t, runErr, string(log))
	assert.Contains(t, string(log), "Creates the cluster: ksail version 7.212.22")
	assert.Contains(t, string(log), "Runs every later step: ksail version candidate")
	assert.Equal(t, "path="+releaseDir+":"+candidateDir+":/usr/bin:/bin\n", string(written))
}

func writeFakeKSail(t *testing.T, dir, version string) string {
	t.Helper()

	path := filepath.Join(dir, "ksail")
	script := []byte("#!/bin/sh\necho '" + version + "'\n")

	// The fake CLI must be executable for the step under test to run it.
	err := os.WriteFile(path, script, 0o700) //nolint:gosec // an executable test binary
	require.NoError(t, err)

	return path
}

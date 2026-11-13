package ciharness_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// A K3s or Vanilla dispatch takes the bare-cluster smoke path, which would ignore the release.
func TestSmokeDispatchRefusesCreateWithRelease(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/system-test-hetzner.yaml")
	validate := findHarnessStep(
		t,
		workflow.Jobs["system-test"].Steps,
		"✅ Validate direct-provider smoke selection",
	)
	assert.Equal(t, "${{ inputs.create_with_release }}", validate.Env["CREATE_WITH_RELEASE"])

	log, err := runStepScript(t, validate.Run, "", []string{
		"DISTRIBUTION=K3s", "CREATE_WITH_RELEASE=v7.212.22",
	})
	require.Error(t, err, log)
	assert.Contains(t, log, "create_with_release=v7.212.22")

	log, err = runStepScript(t, validate.Run, "", []string{"DISTRIBUTION=K3s"})
	require.NoError(t, err, log)
}

type releaseUpdateCase struct {
	name    string
	step    string
	outputs []string
	fail    bool
	wantErr string
}

// On a cluster an earlier release created, the first update may change it in place but never
// recreate it, a failed command is never read as success, and a second update must find
// nothing left to change.
func TestReleaseCreatedClusterUpdateChecks(t *testing.T) {
	t.Parallel()

	steps := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml").Runs.Steps
	dryRun := "🧪 ksail cluster update --dry-run"
	update := "🧪 ksail cluster update"

	for _, name := range []string{dryRun, update} {
		assert.Equal(
			t,
			"${{ inputs.create-binary != '' }}",
			findHarnessStep(t, steps, name).Env["CREATED_BY_RELEASE"],
		)
	}

	for _, testCase := range []releaseUpdateCase{
		{
			name: "dry run with in-place changes", step: dryRun,
			outputs: []string{"Would apply 2 in-place, 0 reboot-required, 0 recreate-required"},
		},
		{
			name: "dry run that would recreate", step: dryRun, wantErr: "would recreate",
			outputs: []string{"Would apply 1 in-place, 0 reboot-required, 1 recreate-required"},
		},
		{name: "failed dry run", step: dryRun, fail: true, wantErr: "dry run failed"},
		{name: "update that converges", step: update, outputs: []string{"Applied 2 in-place changes", "No changes detected"}},
		{
			name: "update that does not converge", step: update, wantErr: "still found changes",
			outputs: []string{"Applied 2 in-place changes", "Applied 1 in-place change"},
		},
		{name: "failed update", step: update, fail: true, wantErr: "update failed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fakeDir := writeSequencedKSail(t, testCase.outputs, testCase.fail)
			log, err := runStepScript(
				t,
				findHarnessStep(t, steps, testCase.step).Run,
				fakeDir,
				[]string{
					"CREATED_BY_RELEASE=true",
					"DISTRIBUTION=Talos",
					"PROVIDER=Hetzner",
					"ARGS=--name demo",
				},
			)

			if testCase.wantErr != "" {
				require.Error(t, err, log)
				assert.Contains(t, log, testCase.wantErr)

				return
			}

			require.NoError(t, err, log)
		})
	}
}

// runStepScript runs a step's script with its log directory moved into a temporary one, a
// fake ksail first on PATH when fakeDir is set, and the given environment.
func runStepScript(t *testing.T, script, fakeDir string, env []string) (string, error) {
	t.Helper()

	logDir := filepath.Join(t.TempDir(), "logs")
	require.NoError(t, os.MkdirAll(logDir, 0o750))

	path := "/usr/bin:/bin"
	if fakeDir != "" {
		path = fakeDir + ":" + path
	}

	script = strings.ReplaceAll(script, "/tmp/ksail-system-test-logs", logDir)

	//nolint:gosec // runs the reviewed workflow step under test
	command := exec.CommandContext(t.Context(), "bash", "-c", script)

	command.Env = append([]string{"PATH=" + path}, env...)
	output, err := command.CombinedOutput()

	return string(output), err
}

// writeSequencedKSail writes a fake ksail that prints the next of outputs on each call, or
// fails every call when fail is set.
func writeSequencedKSail(t *testing.T, outputs []string, fail bool) string {
	t.Helper()

	dir := t.TempDir()
	counter := filepath.Join(dir, "calls")
	require.NoError(t, os.WriteFile(counter, []byte("0"), 0o600))

	var script strings.Builder

	script.WriteString("#!/bin/bash\n")

	if fail {
		script.WriteString("echo 'boom'; exit 3\n")
	} else {
		script.WriteString("n=$(cat '" + counter + "'); echo $((n + 1)) > '" + counter + "'\n")
		script.WriteString("case $n in\n")

		for index, output := range outputs {
			script.WriteString("  " + strconv.Itoa(index) + ") echo '" + output + "' ;;\n")
		}

		script.WriteString("  *) echo 'unexpected call'; exit 4 ;;\nesac\n")
	}

	path := filepath.Join(dir, "ksail")
	content := []byte(script.String())

	err := os.WriteFile(path, content, 0o700) //nolint:gosec // an executable test binary
	require.NoError(t, err)

	return dir
}

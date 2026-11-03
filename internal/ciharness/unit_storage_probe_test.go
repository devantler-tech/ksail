package ciharness_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestUnitStorageProbeHasOneReadOnlyManualJob(t *testing.T) {
	t.Parallel()

	path := ".github/workflows/measure-unit-storage.yaml"
	contents := readRepoFile(t, path)

	var document map[string]any
	require.NoError(t, yaml.Unmarshal(contents, &document))
	events, valid := document["on"].(map[string]any)
	require.True(t, valid)
	require.Len(t, events, 1)
	dispatch, valid := events["workflow_dispatch"].(map[string]any)
	require.True(t, valid)
	inputs, valid := dispatch["inputs"].(map[string]any)
	require.True(t, valid)
	enabled, valid := inputs["enabled"].(map[string]any)
	require.True(t, valid)
	assert.Equal(t, false, enabled["default"])
	assert.Equal(t, "boolean", enabled["type"])
	workflow := readCIWorkflow(t, path)
	require.Len(t, workflow.Jobs, 1)
	job, valid := workflow.Jobs["measure"]
	require.True(t, valid)
	assert.Contains(t, job.If, "github.ref == 'refs/heads/main'")
	assert.Contains(t, job.If, "inputs.enabled == true")
	assert.Equal(t, map[string]string{"contents": "read"}, job.Permissions)
	assert.Equal(t, 30, job.TimeoutMinutes)
	assert.False(t, job.ContinueOnError)
	assert.Contains(t, job.Steps[0].Uses, "step-security/harden-runner@")
	checkout := findHarnessStep(t, job.Steps, "Checkout fixed failed workload")
	assert.Equal(t, "bab0963e3c16b91e8a236e9679a29ffcde276b8f", checkout.With["ref"])
	cache := findHarnessStep(t, job.Steps, "Restore exact control module cache")
	assert.Equal(
		t,
		"go-mod-v1-Linux-X64-.-7c4839f70b57f520b6effc59074c3d5bd9199ccbc893762f484f974b97f58853",
		cache.With["key"],
	)
	assert.NotContains(t, string(contents), "actions/cache/save@")
	assert.NotContains(t, string(contents), "restore-keys:")
	receipt := findHarnessStep(t, job.Steps, "Preserve bounded structured receipt")
	upload := findHarnessStep(t, job.Steps, "Upload structured receipt only")
	assert.Equal(t, receipt.If, upload.If)
	assert.Contains(t, upload.If, "!cancelled()")
}

func TestUnitStorageProbeRefusesLocalExecutionBeforeWriting(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	command := exec.CommandContext(
		t.Context(),
		"bash",
		"../../.github/scripts/unit-storage-probe.sh",
		"snapshot",
		"before-cleanup",
	)

	command.Env = append(os.Environ(), "GITHUB_ACTIONS=false", "RUNNER_TEMP="+directory)
	output, err := command.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "unit storage probe requires GitHub-hosted main")

	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	assert.Empty(t, entries, "admission must precede writes and commands")
}

func TestUnitStorageControlIsTheActualFailedSharedCleanup(t *testing.T) {
	t.Parallel()
	contents := readRepoFile(t, "internal/ciharness/testdata/unit-storage-control/cleanup.run")
	assert.Equal(
		t,
		"de095dc3e3283f2fefe05e10430f89bc22da53965b7877cce2494e760ecb6d71",
		fmt.Sprintf("%x", sha256.Sum256(contents)),
	)
	assert.Contains(t, string(contents), "sudo rm -rf")
	assert.NotContains(t, string(contents), "docker system prune")
	assert.NotContains(t, string(contents), "/usr/local/lib/python")
}

type unitStorageCase struct {
	mode                 string
	exit                 int
	counters, components bool
}

func unitStorageCases() []unitStorageCase {
	return []unitStorageCase{
		{"success", 0, true, true},
		{"command-failure", 7, true, true},
		{"df-failure", 0, false, false},
		{"df-timeout", 0, false, false},
		{"bad-inodes", 0, false, false},
		{"du-failure", 0, true, false},
		{"cleanup-df-failure", 0, false, true},
		{"frame-failure", 0, false, true},
		{"sampler-term", 0, false, true},
		{"sampler-kill", 0, false, true},
		{"sampler-timeout", 0, false, true},
		{"sampler-stuck", 0, false, true},
		{"after-frame-failure", 0, false, false},
		{"after-frame-command-failure", 7, false, false},
		{"final-record-failure", 0, false, false},
		{"final-record-command-failure", 7, false, false},
	}
}

func TestUnitStorageProbePreservesCommandAndUnknownObservations(t *testing.T) {
	t.Parallel()

	for _, testCase := range unitStorageCases() {
		t.Run(testCase.mode, func(t *testing.T) {
			t.Parallel()
			fixture := newUnitStorageFixture(t, testCase.mode)
			fixture.prepare(t)
			runOutput, runErr := fixture.run(t, "run")
			assert.Equal(t, testCase.exit != 0, runErr != nil, "run: %s", runOutput)
			publishOutput, publishErr := fixture.run(t, "publish")
			assert.Equal(
				t,
				!testCase.counters || !testCase.components,
				publishErr != nil,
				"publish: %s",
				publishOutput,
			)

			contents, err := os.ReadFile(filepath.Join(fixture.state, "public", "report.json"))
			require.NoError(t, err)
			assert.NotContains(t, string(contents), fixture.root)
			assert.NotContains(t, string(contents), "PRIVATE_FIXTURE_DETAIL")

			var report struct {
				Complete           bool `json:"countersComplete"`
				ComponentsComplete bool `json:"componentsComplete"`
				Command            struct {
					Executed  bool `json:"executed"`
					Completed bool `json:"completed"`
					Exit      int  `json:"exit"`
				} `json:"command"`
			}
			require.NoError(t, json.Unmarshal(contents, &report))
			assert.Equal(t, testCase.counters, report.Complete)
			assert.Equal(t, testCase.components, report.ComponentsComplete)
			assert.True(t, report.Command.Executed)
			assert.True(t, report.Command.Completed)

			assert.Equal(t, testCase.exit, report.Command.Exit)

			invocation, err := os.ReadFile(filepath.Join(fixture.root, "go-invocation"))
			require.NoError(t, err)
			assert.Equal(t, "test ./...\n", string(invocation))
		})
	}
}

func TestUnitStorageProbeRejectsUnreadableColdCache(t *testing.T) {
	t.Parallel()
	fixture := newUnitStorageFixture(t, "find-failure")
	output, err := fixture.run(t, "prepare")
	require.Error(t, err)
	assert.Contains(t, output, "compilation cache could not be examined")
	assert.NotContains(t, output, "PRIVATE_FIXTURE_DETAIL")

	_, err = os.Stat(filepath.Join(fixture.state, "prepared"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestUnitStorageProbeCannotRunWithoutItsExactCache(t *testing.T) {
	t.Parallel()
	fixture := newUnitStorageFixture(t, "success")
	fixture.prepare(t)
	fixture.env = append(fixture.env, "UNIT_STORAGE_CACHE_HIT=false")
	output, err := fixture.run(t, "run")
	require.Error(t, err)
	assert.Contains(t, output, "exact control cache unavailable")

	_, err = os.Stat(filepath.Join(fixture.root, "go-invocation"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestUnitStorageProbeCannotReuseACompletedObservation(t *testing.T) {
	t.Parallel()
	fixture := newUnitStorageFixture(t, "success")
	fixture.prepare(t)
	output, err := fixture.run(t, "run")
	require.NoError(t, err, output)
	output, err = fixture.run(t, "run")
	require.Error(t, err)
	assert.Contains(t, output, "observation is not reusable")
	output, err = fixture.run(t, "publish")
	require.Error(t, err, output)
	contents, err := os.ReadFile(filepath.Join(fixture.state, "public", "report.json"))
	require.NoError(t, err)
	assert.Contains(t, string(contents), `"countersComplete":false`)
}

func TestUnitStorageProbeAdmissionPrecedesPrivateState(t *testing.T) {
	t.Parallel()

	for _, override := range []string{
		"RUNNER_ENVIRONMENT=self-hosted", "GITHUB_REF=refs/heads/topic", "GITHUB_REPOSITORY=example/other",
	} {
		t.Run(override, func(t *testing.T) {
			t.Parallel()
			fixture := newUnitStorageFixture(t, "success")
			fixture.env = append(fixture.env, override)
			output, err := fixture.run(t, "snapshot", "before-cleanup")
			require.Error(t, err)
			assert.Contains(t, output, "requires GitHub-hosted main")

			_, err = os.Stat(fixture.state)
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestUnitStorageProbeCancellationStopsItsOwnedCommand(t *testing.T) {
	t.Parallel()
	fixture := newUnitStorageFixture(t, "cancel")
	fixture.prepare(t)
	command := unitStorageCommand(t, "run")
	command.Env = fixture.env
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(fixture.root, "go-running"))

		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, command.Process.Signal(syscall.SIGTERM))
	require.Error(t, command.Wait())

	_, err := os.Stat(filepath.Join(fixture.root, "go-terminated"))
	require.NoError(t, err, "the owned test process must receive termination")
	contents, err := os.ReadFile(filepath.Join(fixture.state, "command.json"))
	require.NoError(t, err)

	var outcome struct {
		Completed bool `json:"completed"`
		Exit      int  `json:"exit"`
	}
	require.NoError(t, json.Unmarshal(contents, &outcome))
	assert.False(t, outcome.Completed)
	assert.Equal(t, 143, outcome.Exit)
}

type unitStorageFixture struct {
	root, state string
	env         []string
}

func (fixture unitStorageFixture) run(t *testing.T, arguments ...string) (string, error) {
	t.Helper()
	command := unitStorageCommand(t, arguments...)
	command.Env = fixture.env
	output, err := command.CombinedOutput()

	return string(output), err
}

// Keep subprocess arguments closed: fixtures cannot select another script or
// introduce a shell command through a mode or phase.
func unitStorageCommand(t *testing.T, arguments ...string) *exec.Cmd {
	t.Helper()

	const script = "../../.github/scripts/unit-storage-probe.sh"
	// The fake workload waits for an observed sampler frame, not an assumed
	// scheduling interval. Bound the operation and use the script's tested
	// cancellation trap to stop its owned sampler and workload.
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)

	var command *exec.Cmd

	switch strings.Join(arguments, " ") {
	case "run":
		command = exec.CommandContext(ctx, "bash", script, "run")
	case "prepare":
		command = exec.CommandContext(ctx, "bash", script, "prepare")
	case "publish":
		command = exec.CommandContext(ctx, "bash", script, "publish")
	case "snapshot before-cleanup":
		command = exec.CommandContext(ctx, "bash", script, "snapshot", "before-cleanup")
	case "snapshot after-cleanup":
		command = exec.CommandContext(ctx, "bash", script, "snapshot", "after-cleanup")
	case "snapshot after-setup":
		command = exec.CommandContext(ctx, "bash", script, "snapshot", "after-setup")
	case "snapshot after-restore":
		command = exec.CommandContext(ctx, "bash", script, "snapshot", "after-restore")
	default:
		t.Fatal("unknown fixture operation")

		return nil
	}

	command.Cancel = func() error {
		err := command.Process.Signal(syscall.SIGTERM)
		if err != nil {
			return fmt.Errorf("terminate unit storage fixture: %w", err)
		}

		return nil
	}
	command.WaitDelay = 5 * time.Second

	return command
}

func (fixture unitStorageFixture) prepare(t *testing.T) {
	t.Helper()

	for _, phase := range []string{"before-cleanup", "after-cleanup"} {
		output, err := fixture.run(t, "snapshot", phase)
		require.NoError(t, err, output)
	}

	output, err := fixture.run(t, "prepare")
	require.NoError(t, err, output)

	for _, phase := range []string{"after-setup", "after-restore"} {
		output, err = fixture.run(t, "snapshot", phase)
		require.NoError(t, err, output)
	}
}

func newUnitStorageFixture(t *testing.T, mode string) unitStorageFixture {
	t.Helper()

	jqPath, err := exec.LookPath("jq")
	require.NoError(t, err)

	root := t.TempDir()
	for _, name := range []string{"bin", "runner", ".unit-consumer", "module", "build", "toolchain", "home"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o700))
	}

	env := append(
		os.Environ(),
		"PATH="+filepath.Join(root, "bin")+":"+os.Getenv("PATH"),
		"HOME="+filepath.Join(
			root,
			"home",
		),
		"TMPDIR=/tmp",
		"GITHUB_ACTIONS=true",
		"RUNNER_ENVIRONMENT=github-hosted",
		"RUNNER_OS=Linux",
		"GITHUB_REPOSITORY=devantler-tech/ksail",
		"GITHUB_REF=refs/heads/main",
		"GITHUB_WORKSPACE="+root,
		"RUNNER_TEMP="+filepath.Join(root, "runner"),
		"GITHUB_SHA="+strings.Repeat("a", 40),
		"GITHUB_RUN_ID=123",
		"GITHUB_RUN_ATTEMPT=1",
		"ImageOS=ubuntu26",
		"ImageVersion=20261018.50.1",
		"GITHUB_OUTPUT="+filepath.Join(root, "outputs"),
		"UNIT_STORAGE_CACHE_HIT=true",
		"UNIT_STORAGE_CACHE_KEY=go-mod-v1-Linux-X64-.-7c4839f70b57f520b6effc59074c3d5bd9199ccbc893762f484f974b97f58853",
		"UNIT_STORAGE_FIXTURE_ROOT="+root,
		"UNIT_STORAGE_FIXTURE_MODE="+mode,
		"UNIT_STORAGE_REAL_JQ="+jqPath,
	)
	writeUnitStorageStubs(t, root)

	return unitStorageFixture{
		root,
		filepath.Join(root, "runner", "unit-storage-probe"),
		env,
	}
}

func writeUnitStorageStubs(t *testing.T, root string) {
	t.Helper()

	stubs := map[string]string{
		"jq": unitStorageJQStub(),
		"sleep": `case "$UNIT_STORAGE_FIXTURE_MODE" in
  frame-failure) touch "$UNIT_STORAGE_FIXTURE_ROOT/sampler-after-snapshot" ;;
  sampler-term|sampler-kill)
    printf '%s\n' "$PPID" > "$UNIT_STORAGE_FIXTURE_ROOT/sampler-pid"
    if [[ "$UNIT_STORAGE_FIXTURE_MODE" == sampler-term ]]; then kill -TERM "$PPID"
    else kill -KILL "$PPID"; fi ;;
  sampler-stuck)
    printf '%s\n' "$PPID" > "$UNIT_STORAGE_FIXTURE_ROOT/sampler-pid"
    while kill -0 "$PPID" 2>/dev/null; do
      process_state=$(ps -o stat= -p "$PPID" 2>/dev/null || true)
      [[ "$process_state" != *Z* ]] || break
      /bin/sleep 0.01
    done ;;
  *) exec /bin/sleep "$@" ;;
esac`,
		"git": `if [[ "$*" == *rev-parse* ]]; then echo bab0963e3c16b91e8a236e9679a29ffcde276b8f; fi`,
		"gnutimeout": `[[ "$UNIT_STORAGE_FIXTURE_MODE $*" != 'sampler-timeout --kill-after=5s 240s tail '* ]] || exit 124
[[ "$UNIT_STORAGE_FIXTURE_MODE $*" != 'sampler-stuck '*' tail '* ]] || exit 124
[[ "$1" != --kill-after=* ]] || shift
shift; exec "$@"`,
		"tail": `[[ "$#" == 3 && "$1" == --pid=* && "$2 $3" == '-f /dev/null' ]] || exit 92
pid=${1#--pid=}
[[ "$pid" =~ ^[1-9][0-9]*$ ]] || exit 93
while kill -0 "$pid" 2>/dev/null; do
  process_state=$(ps -o stat= -p "$pid" 2>/dev/null || true)
  [[ "$process_state" != *Z* ]] || break
  /bin/sleep 0.01
done`,
		"stat": `echo 17`,
		"find": `if [[ "$UNIT_STORAGE_FIXTURE_MODE" == find-failure ]]; then
  echo PRIVATE_FIXTURE_DETAIL >&2; exit 1
fi`,
		"du": `[[ "$UNIT_STORAGE_FIXTURE_MODE" != du-failure ]] || exit 1
printf '42\tPRIVATE_FIXTURE_DETAIL\n'`,
		"df": `[[ "$UNIT_STORAGE_FIXTURE_MODE" != df-failure ]] || exit 1
if [[ "$UNIT_STORAGE_FIXTURE_MODE" == cleanup-df-failure &&
      ! -f "$UNIT_STORAGE_FIXTURE_ROOT/runner/unit-storage-probe/prepared" ]]; then exit 1; fi
[[ "$UNIT_STORAGE_FIXTURE_MODE" != df-timeout ]] || exit 124
echo 'Filesystem Blocks Used Available Capacity Mounted'
if [[ "$1" == -Pi ]]; then
  [[ "$UNIT_STORAGE_FIXTURE_MODE" != bad-inodes ]] || { echo 'private 100 200 300 0% /'; exit; }
  echo 'private 10000 2000 8000 20% /'
else echo 'private 100000 20000 80000 20% /'; fi`,
		"go": unitStorageGoStub(),
	}
	for name, body := range stubs {
		writeExecutableStub(
			t,
			filepath.Join(root, "bin", name),
			"#!/usr/bin/env bash\nset -euo pipefail\n"+body+"\n",
		)
	}
}

func unitStorageJQStub() string {
	return `if [[ "$UNIT_STORAGE_FIXTURE_MODE" == frame-failure &&
    " $* " == *' --arg phase during-test '* ]]; then
  printf '%s\n' "$PPID" > "$UNIT_STORAGE_FIXTURE_ROOT/sampler-pid"
  "$UNIT_STORAGE_REAL_JQ" "$@"
  exit 23
fi
if [[ "$UNIT_STORAGE_FIXTURE_MODE" == after-frame-* &&
    " $* " == *' --arg phase after-test '* ]]; then
  "$UNIT_STORAGE_REAL_JQ" "$@"
  exit 23
fi
if [[ "$UNIT_STORAGE_FIXTURE_MODE" == final-record-* &&
    " $* " == *' --argjson sampling_complete '* ]]; then
  "$UNIT_STORAGE_REAL_JQ" "$@"
  exit 23
fi
exec "$UNIT_STORAGE_REAL_JQ" "$@"`
}

func unitStorageGoStub() string {
	return `if [[ "$1" == env ]]; then
  jq -nc --arg root "$UNIT_STORAGE_FIXTURE_ROOT" '{
    GOCACHE:($root+"/build"),GOMODCACHE:($root+"/module"),GOROOT:($root+"/toolchain"),
    GOVERSION:"go1.27.3",GOTOOLCHAIN:"local",CGO_ENABLED:"1",GOFLAGS:"",GOTMPDIR:""}'
else
  printf '%s\n' "$*" > "$UNIT_STORAGE_FIXTURE_ROOT/go-invocation"
  if [[ "$UNIT_STORAGE_FIXTURE_MODE" == cancel ]]; then
    trap 'touch "$UNIT_STORAGE_FIXTURE_ROOT/go-terminated"; exit 143' TERM INT
    touch "$UNIT_STORAGE_FIXTURE_ROOT/go-running"
    for ((i=0;i<300;i++)); do /bin/sleep 0.01; done
    exit 11
  fi
  case "$UNIT_STORAGE_FIXTURE_MODE" in
    sampler-stuck)
      until [[ -s "$UNIT_STORAGE_FIXTURE_ROOT/sampler-pid" ]]; do /bin/sleep 0.01; done ;;
    frame-failure|sampler-term|sampler-kill)
      until [[ -s "$UNIT_STORAGE_FIXTURE_ROOT/sampler-pid" ]]; do /bin/sleep 0.01; done
      sampler=$(cat "$UNIT_STORAGE_FIXTURE_ROOT/sampler-pid")
      [[ "$sampler" =~ ^[1-9][0-9]*$ ]] || exit 91
      until [[ -f "$UNIT_STORAGE_FIXTURE_ROOT/sampler-after-snapshot" ]]; do
        kill -0 "$sampler" 2>/dev/null || break
        process_state=$(ps -o stat= -p "$sampler" 2>/dev/null || true)
        [[ "$process_state" != *Z* ]] || break
        /bin/sleep 0.01
      done ;;
    *)
      until grep -q '"phase":"during-test"' "$UNIT_STORAGE_FIXTURE_ROOT/runner/unit-storage-probe/frames.jsonl"; do
        /bin/sleep 0.01
      done ;;
  esac
  echo PRIVATE_FIXTURE_DETAIL
  [[ "$UNIT_STORAGE_FIXTURE_MODE" != *command-failure ]] || exit 7
fi`
}

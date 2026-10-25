package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type moduleDownloadScenario struct {
	name                               string
	failures, operationExit, downloads int
	operations                         int
	backoff                            string
}

// The desktop release must reach the guarded module download before compilation.
func TestDesktopReleaseUsesModuleDownloadSetup(t *testing.T) {
	t.Parallel()

	steps := readReleaseJobs(t)["desktop"].Steps
	setupIndex, buildIndex := -1, -1

	for index, step := range steps {
		if step.Uses == "./.github/actions/setup-desktop-build" {
			setupIndex = index

			assert.Empty(t, step.If)
			assert.False(t, step.ContinueOnError)
		}

		if strings.Contains(step.Run, "go build") {
			buildIndex = index
		}
	}

	require.NotEqual(t, -1, setupIndex, "desktop release must use the download-protected setup")
	require.NotEqual(t, -1, buildIndex, "test must inspect the real desktop build entry point")
	assert.Less(t, setupIndex, buildIndex, "downloads must precede desktop compilation")
}

// Release builds and the shipped graph audit recover downloads without retrying their verdict.
func TestReleaseModuleDownloadRetries(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")

	jobs := readReleaseJobs(t)
	desktop := readCompositeAction(t, ".github/actions/setup-desktop-build/action.yml")

	targets := map[string][]harnessStep{
		"cli":              jobs["goreleaser"].Steps,
		"macos":            jobs["desktop-macos"].Steps,
		"desktop":          desktop.Runs.Steps,
		"dependency_audit": readCIWorkflow(t, ".github/workflows/ci.yaml").Jobs["release-graph-audit"].Steps,
	}
	for target, steps := range targets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			prefetch := releaseModuleDownloadStep(t, steps)

			downstreamRun := moduleDownloadOperation(t, target, steps)
			for _, scenario := range []moduleDownloadScenario{
				{name: "cache_warm", downloads: 1, operations: 1},
				{name: "two_proxy_blips", failures: 2, downloads: 3, operations: 1, backoff: "5\n10\n"},
				{name: "exhausted", failures: 3, downloads: 3, backoff: "5\n10\n"},
				{name: "downstream_error", operationExit: 37, downloads: 1, operations: 1},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					t.Parallel()
					runModuleDownloadScenario(t, prefetch, downstreamRun, scenario)
				})
			}
		})
	}
}

// Select the real audit invocation or the existing synthetic release-build control.
func moduleDownloadOperation(t *testing.T, target string, steps []harnessStep) string {
	t.Helper()

	if target != "dependency_audit" {
		return "go build"
	}

	prefetch := findHarnessStep(t, steps, "📥 Download Go modules before the audit")
	require.Equal(t, "bash .github/scripts/download-go-modules.sh", strings.TrimSpace(prefetch.Run))

	auditSteps := 0

	for _, step := range steps {
		if step.Name == "🔐 Audit Shipped Dependency Graphs" {
			auditSteps++
		}
	}

	require.Equal(t, 1, auditSteps,
		"workflow must contain exactly one shipped dependency audit step")

	step := findHarnessStep(t, steps, "🔐 Audit Shipped Dependency Graphs")
	require.Equal(t, "go test ./pkg/client/kubescape/... -count=1", strings.TrimSpace(step.Run),
		"audit findings must not be retried or hidden by a shell wrapper")
	require.Empty(t, step.If)
	require.False(t, step.ContinueOnError)

	return step.Run
}

// Require a single unconditional prefetch between Go setup and the downstream operation.
func releaseModuleDownloadStep(t *testing.T, steps []harnessStep) harnessStep {
	t.Helper()

	var prefetch harnessStep

	prefetchIndex, setupIndex, buildIndex := -1, -1, len(steps)
	for index, step := range steps {
		if strings.HasPrefix(step.Uses, "actions/setup-go@") {
			setupIndex = index
		}

		if strings.Contains(step.Run, "download-go-modules.sh") {
			require.Equal(t, -1, prefetchIndex, "one bounded download phase per job")

			prefetch, prefetchIndex = step, index
		}

		if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") ||
			step.Name == "🔐 Audit Shipped Dependency Graphs" {
			buildIndex = index
		}
	}

	require.NotEqual(t, -1, prefetchIndex, "release job must prefetch modules before building")
	require.NotEqual(t, -1, setupIndex, "release job must configure Go before downloading")
	assert.Greater(t, prefetchIndex, setupIndex, "use the configured Go toolchain")
	assert.Less(t, prefetchIndex, buildIndex, "downloads must finish before GoReleaser")
	assert.Empty(t, prefetch.If)
	assert.False(t, prefetch.ContinueOnError)

	return prefetch
}

// Execute the checked-in downloader with recorded transport, backoff and downstream results.
func runModuleDownloadScenario(
	t *testing.T,
	prefetch harnessStep,
	downstreamRun string,
	scenario moduleDownloadScenario,
) {
	t.Helper()

	directory := t.TempDir()
	fixtures, err := os.OpenRoot(directory)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, fixtures.Close()) })
	writeModuleDownloadStubs(t, directory)

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(prefetch.Run + "\n" + downstreamRun)
	command.Dir = root
	command.Env = append(os.Environ(),
		"PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_ACTION_PATH="+filepath.Join(root, ".github/actions/setup-desktop-build"),
		"DOWNLOAD_CALLS="+filepath.Join(directory, "calls"),
		"DOWNLOAD_DELAYS="+filepath.Join(directory, "delays"),
		"DOWNLOAD_FAILURES="+strconv.Itoa(scenario.failures),
		"OPERATION_EXIT="+strconv.Itoa(scenario.operationExit))

	output, runErr := command.CombinedOutput()
	assertModuleDownloadResult(t, scenario, string(output), runErr)

	actual, err := fixtures.ReadFile("calls")
	require.NoError(t, err)
	assert.Equal(t, scenario.downloads, strings.Count(string(actual), "download\n"))
	assert.Equal(t, scenario.operations, strings.Count(string(actual), "operation\n"))

	if scenario.backoff != "" {
		actualDelays, readErr := fixtures.ReadFile("delays")
		require.NoError(t, readErr)
		assert.Equal(t, scenario.backoff, string(actualDelays))
	} else {
		assert.NoFileExists(
			t,
			filepath.Join(directory, "delays"),
			"successful downloads must not sleep",
		)
	}
}

// Distinguish exhausted downloads from the original downstream command's failure.
func assertModuleDownloadResult(
	t *testing.T,
	scenario moduleDownloadScenario,
	output string,
	err error,
) {
	t.Helper()

	if scenario.failures < 3 && scenario.operationExit == 0 {
		require.NoErrorf(t, err, "recovery failed: %s", output)

		return
	}

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "failure must propagate")

	if scenario.failures == 3 {
		assert.Equal(t, 1, exitErr.ExitCode())
		assert.Contains(t, output, "Go module download")
		assert.Contains(t, output, "3 attempts")
	} else {
		assert.Equal(
			t,
			scenario.operationExit,
			exitErr.ExitCode(),
			"retain the downstream command's actual failure",
		)
	}
}

// Replace network and sleep effects while retaining the production retry script's decisions.
func writeModuleDownloadStubs(t *testing.T, directory string) {
	t.Helper()
	writeExecutableStub(t, filepath.Join(directory, "go"), `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
"mod download")
	count=0
	if [[ -f "$DOWNLOAD_CALLS" ]]; then count=$(wc -l < "$DOWNLOAD_CALLS"); fi
	printf 'download\n' >> "$DOWNLOAD_CALLS"
	[[ "$count" -ge "$DOWNLOAD_FAILURES" ]]
	;;
"build"|"test ./pkg/client/kubescape/... -count=1")
	printf 'operation\n' >> "$DOWNLOAD_CALLS"
	exit "$OPERATION_EXIT"
	;;
*) printf 'unexpected Go command: %s\n' "$*" >&2; exit 64 ;;
esac
`)
	writeExecutableStub(t, filepath.Join(directory, "sleep"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$DOWNLOAD_DELAYS"
`)
}

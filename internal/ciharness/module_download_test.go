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
	name                           string
	failures, buildExit, downloads int
	builds                         int
	backoff                        string
}

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

// Every release path must recover downloads without retrying compilation.
func TestReleaseModuleDownloadRetries(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")

	jobs := readReleaseJobs(t)
	desktop := readCompositeAction(t, ".github/actions/setup-desktop-build/action.yml")

	targets := map[string][]harnessStep{
		"cli":     jobs["goreleaser"].Steps,
		"macos":   jobs["desktop-macos"].Steps,
		"desktop": desktop.Runs.Steps,
	}
	for target, steps := range targets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			prefetch := releaseModuleDownloadStep(t, steps)
			for _, scenario := range []moduleDownloadScenario{
				{name: "cache_warm", downloads: 1, builds: 1},
				{name: "two_proxy_blips", failures: 2, downloads: 3, builds: 1, backoff: "5\n10\n"},
				{name: "exhausted", failures: 3, downloads: 3, backoff: "5\n10\n"},
				{name: "compiler_error", buildExit: 37, downloads: 1, builds: 1},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					t.Parallel()
					runModuleDownloadScenario(t, prefetch, scenario)
				})
			}
		})
	}
}

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

		if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
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

func runModuleDownloadScenario(
	t *testing.T,
	prefetch harnessStep,
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
	command.Stdin = strings.NewReader(prefetch.Run + "\ngo build")
	command.Dir = root
	command.Env = append(os.Environ(),
		"PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_ACTION_PATH="+filepath.Join(root, ".github/actions/setup-desktop-build"),
		"DOWNLOAD_CALLS="+filepath.Join(directory, "calls"),
		"DOWNLOAD_DELAYS="+filepath.Join(directory, "delays"),
		"DOWNLOAD_FAILURES="+strconv.Itoa(scenario.failures),
		"BUILD_EXIT="+strconv.Itoa(scenario.buildExit))

	output, runErr := command.CombinedOutput()
	assertModuleDownloadResult(t, scenario, string(output), runErr)

	actual, err := fixtures.ReadFile("calls")
	require.NoError(t, err)
	assert.Equal(t, scenario.downloads, strings.Count(string(actual), "download\n"))
	assert.Equal(t, scenario.builds, strings.Count(string(actual), "build\n"))

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

func assertModuleDownloadResult(
	t *testing.T,
	scenario moduleDownloadScenario,
	output string,
	err error,
) {
	t.Helper()

	if scenario.failures < 3 && scenario.buildExit == 0 {
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
			scenario.buildExit,
			exitErr.ExitCode(),
			"retain the compiler's actual failure",
		)
	}
}

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
"build")
	printf 'build\n' >> "$DOWNLOAD_CALLS"
	exit "$BUILD_EXIT"
	;;
*) printf 'unexpected Go command: %s\n' "$*" >&2; exit 64 ;;
esac
`)
	writeExecutableStub(t, filepath.Join(directory, "sleep"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$DOWNLOAD_DELAYS"
`)
}

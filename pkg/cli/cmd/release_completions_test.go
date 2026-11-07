package cmd_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"text/template"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Removing the release hook, selecting a foreign binary, or losing any shell output must
// break this test. The subprocess reuses this package's built test executable and runs
// the actual KSail command tree, avoiding another full CLI build.
func TestReleaseCompletionGeneration(t *testing.T) {
	t.Parallel()

	fixture := newReleaseCompletionFixture(t, runtime.GOOS, runtime.GOOS)
	output, err := fixture.run(t, "", "")
	require.NoError(t, err, output)

	for _, shell := range []string{"bash", "zsh", "fish"} {
		data, readErr := fixture.root.ReadFile("dist/completions/ksail." + shell)
		require.NoError(t, readErr)
		require.NotEmpty(t, data)
		require.Contains(t, string(data), "ksail")
		require.Contains(t, string(data), "__complete")
	}
}

// releaseCompletionFixture is an isolated copy of the release hook, its script and a fake
// native binary, plus the environment the hook runs under.
type releaseCompletionFixture struct {
	root *os.Root
	env  []string
}

// isNativeReleaseHost mirrors the host matrix in .github/scripts/generate-completions.sh.
func isNativeReleaseHost(host string) bool {
	return slices.Contains([]string{
		"darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64",
	}, host)
}

// newReleaseCompletionFixture renders the real release hook for targetOS on hostOS. It skips
// when the actual host cannot run the hook natively: an explicitly supplied foreign hostOS
// is still exercised, so the unsupported-host test keeps running everywhere bash exists.
func newReleaseCompletionFixture(t *testing.T, targetOS, hostOS string) releaseCompletionFixture {
	t.Helper()

	_, bashErr := exec.LookPath("bash")
	if bashErr != nil {
		t.Skip("the release hook needs bash")
	}

	if hostOS == runtime.GOOS && !isNativeReleaseHost(runtime.GOOS+"/"+runtime.GOARCH) {
		t.Skipf("%s/%s is not a native release host", runtime.GOOS, runtime.GOARCH)
	}

	repository, err := os.OpenRoot("../../..")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })

	hookCommand := readReleaseCompletionHook(t, repository)

	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	require.NoError(t, root.MkdirAll(".github/scripts", 0o700))

	script, err := repository.ReadFile(".github/scripts/generate-completions.sh")
	require.NoError(t, err)
	require.NoError(t, root.WriteFile(".github/scripts/generate-completions.sh", script, 0o600))
	require.NoError(t, root.WriteFile("ksail", []byte(completionProcessScript), 0o700))

	executable, err := os.Executable()
	require.NoError(t, err)

	context := map[string]any{
		"Path":    filepath.Join(directory, "ksail"),
		"Os":      targetOS,
		"Arch":    runtime.GOARCH,
		"Runtime": map[string]string{"Goos": hostOS, "Goarch": runtime.GOARCH},
	}
	hookTemplate, err := template.New("release hook").Parse(hookCommand)
	require.NoError(t, err)

	var hook bytes.Buffer
	require.NoError(t, hookTemplate.Execute(&hook, context))
	require.NoError(t, root.WriteFile("release-hook.sh", hook.Bytes(), 0o600))

	return releaseCompletionFixture{
		root: root,
		env: append(os.Environ(),
			"KSAIL_TEST_COMPLETION_PROCESS=1",
			"KSAIL_TEST_COMPLETION_EXECUTABLE="+executable,
			"HOME="+filepath.Join(directory, "home"),
			"XDG_CONFIG_HOME="+filepath.Join(directory, "home"),
			"KUBECONFIG="+filepath.Join(directory, "kubeconfig")),
	}
}

// readReleaseCompletionHook returns the single post-build hook command from .goreleaser.yaml.
func readReleaseCompletionHook(t *testing.T, repository *os.Root) string {
	t.Helper()

	data, err := repository.ReadFile(".goreleaser.yaml")
	require.NoError(t, err)

	var config struct {
		Builds []struct {
			Hooks struct {
				Post []struct {
					Cmd string `yaml:"cmd"`
				} `yaml:"post"`
			} `yaml:"hooks"`
		} `yaml:"builds"`
	}
	require.NoError(t, yaml.Unmarshal(data, &config))
	require.Len(t, config.Builds, 1)
	require.Len(t, config.Builds[0].Hooks.Post, 1,
		"release must generate completion files after building")

	return config.Builds[0].Hooks.Post[0].Cmd
}

// run executes the rendered hook, optionally making one shell fail or print nothing.
func (fixture releaseCompletionFixture) run(
	t *testing.T,
	failedShell, emptyShell string,
) (string, error) {
	t.Helper()

	command := exec.CommandContext(t.Context(), "bash", "release-hook.sh")
	command.Dir = fixture.root.Name()
	command.Env = append(slices.Clone(fixture.env),
		"KSAIL_TEST_COMPLETION_FAIL="+failedShell,
		"KSAIL_TEST_COMPLETION_EMPTY="+emptyShell,
		// Stands in for a release credential the runner holds; the hook must not pass it on.
		"KSAIL_RELEASE_CREDENTIAL_CANARY=present")
	output, err := command.CombinedOutput()

	return string(output), err
}

const completionProcessScript = `#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${KSAIL_RELEASE_CREDENTIAL_CANARY-}" ]]; then echo "runner environment leaked into completion" >&2; exit 24; fi
if [[ -n "${KSAIL_TEST_COMPLETION_FAIL-}" && "$KSAIL_TEST_COMPLETION_FAIL" == "$2" ]]; then exit 23; fi
if [[ -n "${KSAIL_TEST_COMPLETION_EMPTY-}" && "$KSAIL_TEST_COMPLETION_EMPTY" == "$2" ]]; then exit 0; fi
exec "$KSAIL_TEST_COMPLETION_EXECUTABLE" -test.run='^TestReleaseCompletionProcess$' -- "$@"
`

// TestReleaseCompletionProcess is the subprocess entry point the fake binary re-executes.
func TestReleaseCompletionProcess(t *testing.T) {
	t.Parallel()

	if os.Getenv("KSAIL_TEST_COMPLETION_PROCESS") != "1" {
		return
	}

	index := slices.Index(os.Args, "--")
	require.Positive(t, index)

	root := cmd.NewRootCmd("test", "test", "test")
	root.SetArgs(os.Args[index+1:])
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	err := root.Execute()
	if err != nil {
		os.Exit(1)
	}

	// Stop the test runner from appending PASS to a generated completion file.
	os.Exit(0)
}

// TestReleaseCompletionFailurePublishesNoPartialFiles proves a failed or empty shell leaves no output.
func TestReleaseCompletionFailurePublishesNoPartialFiles(t *testing.T) {
	t.Parallel()

	for _, empty := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "command failure", true: "empty output"}[empty],
			func(t *testing.T) {
				t.Parallel()

				fixture := newReleaseCompletionFixture(t, runtime.GOOS, runtime.GOOS)

				failedShell, emptyShell := "zsh", ""
				if empty {
					failedShell, emptyShell = "", "zsh"
				}

				output, err := fixture.run(t, failedShell, emptyShell)
				require.Error(t, err, output)

				_, err = fixture.root.Stat("dist/completions")
				require.ErrorIs(
					t,
					err,
					os.ErrNotExist,
					"incomplete outputs must never reach an archive",
				)
			},
		)
	}
}

// TestReleaseCompletionRefusesExistingOutput proves an existing output directory is never replaced.
func TestReleaseCompletionRefusesExistingOutput(t *testing.T) {
	t.Parallel()

	fixture := newReleaseCompletionFixture(t, runtime.GOOS, runtime.GOOS)
	require.NoError(t, fixture.root.MkdirAll("dist/completions", 0o700))
	require.NoError(t, fixture.root.WriteFile("dist/completions/sentinel", []byte("retain"), 0o600))

	output, err := fixture.run(t, "", "")
	require.Error(t, err, output)
	data, err := fixture.root.ReadFile("dist/completions/sentinel")
	require.NoError(t, err)
	require.Equal(t, "retain", strings.TrimSpace(string(data)))
}

// TestReleaseCompletionForeignTargetNeverExecutes proves a non-host target binary is never run.
func TestReleaseCompletionForeignTargetNeverExecutes(t *testing.T) {
	t.Parallel()

	targetOS := "linux"
	if runtime.GOOS == targetOS {
		targetOS = "darwin"
	}

	fixture := newReleaseCompletionFixture(t, targetOS, runtime.GOOS)
	output, err := fixture.run(t, "bash", "")
	require.NoError(t, err, output)
	_, err = fixture.root.Stat("dist/completions")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestReleaseCompletionCommandTree proves the fake binary serves the real KSail command tree.
func TestReleaseCompletionCommandTree(t *testing.T) {
	t.Parallel()

	fixture := newReleaseCompletionFixture(t, runtime.GOOS, runtime.GOOS)
	command := exec.CommandContext(t.Context(), "bash", "ksail", "__complete", "")
	command.Dir = fixture.root.Name()
	command.Env = fixture.env
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "cluster\t")
	require.Contains(t, string(output), "workload\t")
}

// TestReleaseCompletionUnsupportedHostFails proves a host outside the release matrix is refused.
func TestReleaseCompletionUnsupportedHostFails(t *testing.T) {
	t.Parallel()

	fixture := newReleaseCompletionFixture(t, runtime.GOOS, "freebsd")
	output, err := fixture.run(t, "", "")
	require.Error(t, err,
		"a host with no native release target must not publish empty archives: %s", output)
}

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

type homeGuardCase struct {
	name, mutation, diagnostic string
	exitCode                   int
	wantFailure                bool
}

// Execute the actual workflow fence. Discarding a failed Go command must not
// turn an unexamined home into a verified one, and retaining its status must
// not short-circuit the existing canary diagnostics or cleanup.
func TestHomeIsolationGuardWorkflow(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	step := findHarnessStep(t, workflow.Jobs["home-isolation"].Steps,
		"🏠 Verify tests never touch the real home directory")

	for _, test := range []homeGuardCase{
		{name: "successful unchanged home"},
		{
			name: "startup failure", exitCode: 74, wantFailure: true,
			diagnostic: "Failed test package: github.com/devantler-tech/ksail/v7/internal/ciharness",
		},
		{name: "test failure", exitCode: 1, wantFailure: true},
		{name: "interrupted execution", exitCode: 130, wantFailure: true},
		{
			name: "changed kubeconfig", mutation: "kube", wantFailure: true,
			diagnostic: "A test mutated $HOME/.kube/config",
		},
		{
			name: "changed history", mutation: "history", wantFailure: true,
			diagnostic: "A test mutated $HOME/.ksail/switch-history.json",
		},
		{
			name: "added home file", mutation: "add", wantFailure: true,
			diagnostic: "A test created or removed files",
		},
		{
			name: "deleted kubeconfig", mutation: "delete-kube", wantFailure: true,
			diagnostic: "A test mutated $HOME/.kube/config",
		},
		{
			name: "deleted history", mutation: "delete-history", wantFailure: true,
			diagnostic: "A test mutated $HOME/.ksail/switch-history.json",
		},
		{
			name: "deleted canary directory", mutation: "delete-dir", wantFailure: true,
			diagnostic: "A test created or removed files",
		},
		{
			name: "failure still checks canaries", exitCode: 74, mutation: "kube", wantFailure: true,
			diagnostic: "A test mutated $HOME/.kube/config",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			exerciseHomeIsolationGuard(t, step.Run, test)
		})
	}
}

func exerciseHomeIsolationGuard(t *testing.T, script string, test homeGuardCase) {
	t.Helper()

	command, fixture := homeIsolationGuardCommand(t, script, test.mutation, test.exitCode)
	directory, err := os.OpenRoot(fixture)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, directory.Close()) })

	output, runErr := command.CombinedOutput()
	control, readErr := directory.ReadFile("control")
	require.NoError(t, readErr, "the Go boundary must reach the configured result: %s", output)
	require.Equal(t, strconv.Itoa(test.exitCode), string(control))

	if test.wantFailure {
		require.Error(t, runErr, "an unverified or changed home must fail: %s", output)
	} else {
		require.NoError(t, runErr, "%s", output)
	}

	if test.diagnostic != "" {
		assert.Contains(t, string(output), test.diagnostic)
	}

	assert.NotContains(t, string(output), "PRIVATE_FIXTURE_DETAIL")

	// This boundary records only after checking redirected HOME and external
	// cache paths. An early shell failure cannot satisfy a refusal case, and
	// a cached test verdict cannot satisfy the isolation guard.
	arguments, readErr := directory.ReadFile("arguments")
	require.NoError(t, readErr)
	assert.Equal(t, "test\n-count=1\n./...\n", string(arguments))

	for _, name := range []string{"tmp", "real-home"} {
		entries, readErr := os.ReadDir(filepath.Join(fixture, name))
		require.NoError(t, readErr)
		assert.Empty(t, entries, "%s must remain empty after the guard", name)
	}
}

func homeIsolationGuardCommand(
	t *testing.T,
	script, mutation string,
	exitCode int,
) (*exec.Cmd, string) {
	t.Helper()

	fixture := t.TempDir()
	for _, directory := range []string{"bin", "tmp", "real-home"} {
		require.NoError(t, os.Mkdir(filepath.Join(fixture, directory), 0o700))
	}

	mktemp, err := exec.LookPath("mktemp")
	require.NoError(t, err)
	writeExecutableStub(t, filepath.Join(fixture, "bin", "go"), homeGuardGoBoundary)
	// An explicit template with the real utility keeps every temporary file
	// inside this fixture on both developer OSes and CI.
	writeExecutableStub(t, filepath.Join(fixture, "bin", "mktemp"), `#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 1 && "$1" == -d ]]
exec "$HOME_GUARD_MKTEMP" -d "$HOME_GUARD_FIXTURE/tmp/sandbox.XXXXXXXX"
`)

	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(script)

	command.Env = append(os.Environ(),
		"PATH="+filepath.Join(fixture, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+filepath.Join(fixture, "real-home"),
		"TMPDIR="+filepath.Join(fixture, "tmp"),
		"HOME_GUARD_FIXTURE="+fixture,
		"HOME_GUARD_MKTEMP="+mktemp,
		"HOME_GUARD_EXIT="+strconv.Itoa(exitCode),
		"HOME_GUARD_MUTATION="+mutation,
	)

	return command, fixture
}

// Go is the expensive external boundary; all shell fencing, hashing, directory
// comparisons and cleanup execute unchanged from the repository's workflow.
const homeGuardGoBoundary = `#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  env)
    case "$2" in
      GOCACHE) printf '%s/cache\n' "$HOME_GUARD_FIXTURE" ;;
      GOMODCACHE) printf '%s/modules\n' "$HOME_GUARD_FIXTURE" ;;
      GOPATH) printf '%s/gopath\n' "$HOME_GUARD_FIXTURE" ;;
      *) exit 75 ;;
    esac
    ;;
  test)
    [[ "$HOME" == "$HOME_GUARD_FIXTURE"/tmp/* ]] || {
      printf 'Unexpected home: %s (fixture %s)\n' "$HOME" "$HOME_GUARD_FIXTURE" >&2
      exit 75
    }
    [[ "$GOCACHE" == "$HOME_GUARD_FIXTURE/cache" ]]
    [[ "$GOMODCACHE" == "$HOME_GUARD_FIXTURE/modules" ]]
    [[ "$GOPATH" == "$HOME_GUARD_FIXTURE/gopath" ]]
    [[ -s "$HOME/.kube/config" && -s "$HOME/.ksail/switch-history.json" ]]
    printf '%s\n' "$@" > "$HOME_GUARD_FIXTURE/arguments"
    case "$HOME_GUARD_MUTATION" in
      kube) printf 'changed\n' > "$HOME/.kube/config" ;;
      history) printf 'changed\n' > "$HOME/.ksail/switch-history.json" ;;
      add) printf 'new\n' > "$HOME/.kube/new-file" ;;
      delete-kube) rm "$HOME/.kube/config" ;;
      delete-history) rm "$HOME/.ksail/switch-history.json" ;;
      delete-dir) rm -rf "$HOME/.ksail" ;;
      '') ;;
      *) exit 75 ;;
    esac
    printf '%s' "$HOME_GUARD_EXIT" > "$HOME_GUARD_FIXTURE/control"
    if [[ "$HOME_GUARD_EXIT" != 0 ]]; then
      printf 'FAIL\tgithub.com/devantler-tech/ksail/v7/internal/ciharness\t0.001s\n'
      printf 'PRIVATE_FIXTURE_DETAIL\n'
      for ((i = 0; i < 100; i++)); do printf 'package summary\n'; done
      printf 'PRIVATE_FIXTURE_DETAIL\n'
    fi
    exit "$HOME_GUARD_EXIT"
    ;;
  *) exit 75 ;;
esac
`

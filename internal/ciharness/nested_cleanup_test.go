package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nestedCleanupKubectlStub = `#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >> "$FIXTURE_CALLS"
if [[ "${1:-}" == --context ]]; then
  [[ "$2" == fixture-host ]] || exit 99
  shift 2
fi
case "$1 $2" in
  'config current-context') echo fixture-host ;;
  'config use-context') [[ "$FIXTURE_MODE" != host-error ]] ;;
  'get ns'|'get namespace')
    case "$FIXTURE_MODE" in
      query-error) echo 'Forbidden: fixture namespace read' >&2; exit 1 ;;
      pending) echo namespace/ksail-nested-vanilla ;;
      unrelated)
        if [[ "$2" == ns ]]; then
          echo namespace/ksail-nested-vanilla-unrelated
        fi ;;
    esac ;;
  *) exit 99 ;;
esac
`

const nestedCleanupKSailStub = `#!/usr/bin/env bash
set -euo pipefail
printf 'ksail %s\n' "$*" >> "$FIXTURE_CALLS"
if [[ "$FIXTURE_MODE" == delete-error && "$2" == delete ]]; then
  echo 'fixture delete failed' >&2
  exit 1
fi
if [[ "$FIXTURE_MODE" == create-error && "$2" == create && "$*" == *nested-vanilla* ]]; then
  echo 'fixture create failed' >&2
  exit 1
fi
`

const nestedCleanupTimeoutStub = `#!/usr/bin/env bash
set -euo pipefail
printf 'timeout %s\n' "$*" >> "$FIXTURE_CALLS"
shift
if [[ "$FIXTURE_MODE" == query-timeout && "$1" == kubectl ]]; then
  exit 124
fi
exec "$@"
`

func runNestedCleanup(t *testing.T, mode string) (string, string, error) {
	t.Helper()
	version, err := exec.CommandContext(t.Context(), "bash", "-c", `printf '%s' "${BASH_VERSINFO[0]}"`).
		Output()
	require.NoError(t, err)

	if string(version) == "3" {
		t.Skip("the Ubuntu composite action requires Bash 4 or newer")
	}

	action := readCompositeAction(t, ".github/actions/ksail-test-kubernetes-provider/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🧪 Test Kubernetes Provider (nested clusters)")
	dir := t.TempDir()
	logAssignment := `LOG_DIR="/tmp/ksail-system-test-logs/kubernetes-provider"`
	require.Equal(t, 1, strings.Count(step.Run, logAssignment))
	script := strings.Replace(step.Run, logAssignment, `LOG_DIR="$FIXTURE_LOG_DIR"`, 1)
	// Advance the actual Bash loop's clock without a two-minute test or changed production deadline.
	script = "sleep() { SECONDS=$((SECONDS + 121)); }\n" + script

	writeExecutableStub(t, filepath.Join(dir, "kubectl"), nestedCleanupKubectlStub)
	writeExecutableStub(t, filepath.Join(dir, "ksail"), nestedCleanupKSailStub)
	writeExecutableStub(t, filepath.Join(dir, "timeout"), nestedCleanupTimeoutStub)
	command := exec.CommandContext(t.Context(), "bash")
	command.Stdin = strings.NewReader(script)
	command.Dir = filepath.Join("..", "..")
	command.Env = nestedCleanupEnvironment(dir, mode)
	output, err := command.CombinedOutput()
	root, openErr := os.OpenRoot(dir)
	require.NoError(t, openErr)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	calls := readOptionalVersionFixture(t, root, "calls")

	return string(output), string(calls), err
}

func nestedCleanupEnvironment(dir, mode string) []string {
	return append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"DISTRIBUTIONS=Vanilla,K3s", "TIMEOUT=1200", "CNI=", "ASSERT_K3K_SERVER_CREATOR=false",
		"DOCKERHUB_USERNAME=", "DOCKERHUB_TOKEN=", "GHCR_USERNAME=", "GHCR_TOKEN=",
		"FIXTURE_MODE="+mode, "FIXTURE_CALLS="+filepath.Join(dir, "calls"),
		"FIXTURE_LOG_DIR="+filepath.Join(dir, "logs"),
	)
}

func TestNestedCleanupRejectsFailedOrUnverifiedCleanup(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		mode string
		stop bool
	}{
		{mode: "delete-error"},
		{mode: "query-error", stop: true},
		{mode: "query-timeout", stop: true},
		{mode: "pending", stop: true},
		{mode: "host-error", stop: true},
	} {
		t.Run(testCase.mode, func(t *testing.T) {
			t.Parallel()
			output, calls, err := runNestedCleanup(t, testCase.mode)
			require.Error(t, err, output)
			assert.NotContains(t, output, "All Kubernetes provider tests passed")
			assert.Contains(
				t,
				calls,
				"ksail cluster delete --provider Kubernetes --name nested-vanilla --force",
			)

			if testCase.stop {
				assert.NotContains(t, calls, "ksail cluster create --distribution K3s")
				assert.NotContains(t, output, "Vanilla resources drained")
			}
		})
	}
}

func TestNestedCleanupReadsOnlyExactNamespacesOnOriginalHost(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"empty", "unrelated"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			output, calls, err := runNestedCleanup(t, mode)
			require.NoError(t, err, output)
			assert.Contains(t, output, "All Kubernetes provider tests passed")
			assert.Contains(t, calls, "ksail cluster create --distribution K3s")
			assert.Regexp(t, `timeout [1-9][0-9]*s kubectl --context fixture-host`, calls)

			for _, name := range []string{"vanilla", "k3s"} {
				assert.Contains(t, calls, "kubectl --context fixture-host get namespace "+
					"ksail-nested-"+name+" k3k-nested-"+name+" vcluster-nested-"+name+" --ignore-not-found -o name")
			}
		})
	}
}

func TestNestedCleanupPreservesCreateFailureAndStillDeletes(t *testing.T) {
	t.Parallel()
	output, calls, err := runNestedCleanup(t, "create-error")
	require.Error(t, err, output)
	assert.Contains(
		t,
		calls,
		"ksail cluster delete --provider Kubernetes --name nested-vanilla --force",
	)
	assert.NotContains(t, calls, "ksail cluster info --name nested-vanilla")
	assert.NotContains(t, output, "All Kubernetes provider tests passed")
}

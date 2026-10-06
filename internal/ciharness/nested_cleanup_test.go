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
  if [[ "$2" != fixture-host ]]; then
    case "$2" in
      kind-nested-vanilla|k3k-nested-k3s|vcluster-nested-vcluster|admin@nested-talos|kwok-nested-kwok) ;;
      *) exit 99 ;;
    esac
    [[ "$3 ${4:-}" == 'get --raw=/readyz' ]] || exit 99
    case "$FIXTURE_MODE" in
      ready-error) echo 'fixture nested API unreachable' >&2; exit 1 ;;
      ready-ok-error) echo ok; exit 1 ;;
      ready-hang) echo ok; trap '' TERM; exec sleep 60 ;;
      ready-invalid) echo 'not ready' ;;
      *) echo ok ;;
    esac
    exit 0
  fi
  shift 2
fi
case "$1 $2" in
  'config current-context') echo fixture-host ;;
  'config use-context') [[ "$FIXTURE_MODE" != host-error ]] ;;
  'get --raw=/readyz') echo ok ;;
  '-n ksail-nested-talos')
    case "${3:-}" in
      get|exec) echo 'fixture endpoint diagnostics' ;;
      *) exit 99 ;;
    esac ;;
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
if [[ "$2" == info && "$FIXTURE_MODE" == ready-error ]]; then
  echo 'Status: UNKNOWN'
  echo 'Ready: 0/0'
  echo 'Kubernetes API: unreachable'
fi
if [[ "$2" == info && "$FIXTURE_MODE" == info-hang ]]; then
  echo 'Status: RUNNING'
  trap '' TERM
  exec sleep 60
fi
`

const nestedCleanupTimeoutStub = `#!/usr/bin/env bash
set -euo pipefail
printf 'timeout %s\n' "$*" >> "$FIXTURE_CALLS"
if [[ "${1:-}" == --kill-after=* ]]; then
  shift
fi
shift
if [[ "$FIXTURE_MODE" == ready-timeout && "$*" == *--raw=/readyz* ]]; then
  echo ok
  exit 124
fi
if [[ "$FIXTURE_MODE" == ready-hang && "$*" == *--raw=/readyz* ]]; then
  exec "$FIXTURE_TIMEOUT" --kill-after=0.1s 0.2s "$@"
fi
if [[ "$FIXTURE_MODE" == info-hang && "$*" == *'cluster info'* ]]; then
  exec "$FIXTURE_TIMEOUT" --kill-after=0.1s 0.2s "$@"
fi
if [[ "$FIXTURE_MODE" == query-timeout && "$1" == kubectl ]]; then
  exit 124
fi
exec "$@"
`

func runNestedCleanup(t *testing.T, mode string, distributions ...string) (string, string, error) {
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
	script = "trap '' TERM\nsleep() { SECONDS=$((SECONDS + 121)); }\n" + script

	writeExecutableStub(t, filepath.Join(dir, "kubectl"), nestedCleanupKubectlStub)
	writeExecutableStub(t, filepath.Join(dir, "ksail"), nestedCleanupKSailStub)
	writeExecutableStub(t, filepath.Join(dir, "timeout"), nestedCleanupTimeoutStub)
	timeoutPath := nestedFixtureTimeoutPath(t)

	command := exec.CommandContext(t.Context(), "timeout", "--kill-after=0.1s", "3s", "bash")
	if filepath.Base(timeoutPath) == "gtimeout" {
		command = exec.CommandContext(t.Context(), "gtimeout", "--kill-after=0.1s", "3s", "bash")
	}

	command.Stdin = strings.NewReader(script)
	command.Dir = filepath.Join("..", "..")
	command.Env = nestedCleanupEnvironment(dir, mode)
	command.Env = append(command.Env, "FIXTURE_TIMEOUT="+timeoutPath)

	if len(distributions) > 0 {
		command.Env = append(command.Env, "DISTRIBUTIONS="+distributions[0])
	}

	output, err := command.CombinedOutput()
	root, openErr := os.OpenRoot(dir)
	require.NoError(t, openErr)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	calls := readOptionalVersionFixture(t, root, "calls")

	return string(output), string(calls), err
}

func nestedFixtureTimeoutPath(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("timeout")
	if err != nil {
		path, err = exec.LookPath("gtimeout")
	}

	require.NoError(t, err, "GNU timeout is required for the process-bound regression")

	return path
}

func TestNestedReadinessRejectsUnreachableAPIDespiteInfoSuccess(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"ready-error", "ready-ok-error", "ready-invalid", "ready-timeout", "ready-hang"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			output, calls, err := runNestedCleanup(t, mode)
			require.Error(t, err, output)
			assert.NotContains(t, output, "All Kubernetes provider tests passed")
			assert.Contains(t, calls, "kubectl --context kind-nested-vanilla get --raw=/readyz")
			assert.Contains(
				t,
				calls,
				"ksail cluster delete --provider Kubernetes --name nested-vanilla --force",
			)
			assert.Contains(t, output, "Vanilla resources drained")
			assert.Contains(t, output, "nested API readiness FAILED")
		})
	}
}

func TestNestedReadinessBindsEachDistributionContext(t *testing.T) {
	t.Parallel()
	output, calls, err := runNestedCleanup(t, "empty", "Vanilla,K3s,VCluster,Talos,KWOK")
	require.NoError(t, err, output)
	assert.Contains(t, output, "All Kubernetes provider tests passed")

	for _, contextName := range []string{
		"kind-nested-vanilla", "k3k-nested-k3s", "vcluster-nested-vcluster", "admin@nested-talos", "kwok-nested-kwok",
	} {
		assert.Contains(t, calls, "timeout --kill-after=1s 20s kubectl --context "+
			contextName+" get --raw=/readyz --request-timeout=15s")
	}

	assert.NotContains(t, calls, "kubectl --context fixture-host get --raw=/readyz")
}

func TestNestedReadinessReportsAPIErrorBeforeInfo(t *testing.T) {
	t.Parallel()
	output, calls, err := runNestedCleanup(t, "ready-error", "Talos")
	require.Error(t, err, output)
	assert.Contains(t, output, "fixture nested API unreachable")
	assert.Contains(t, calls, "kubectl --context admin@nested-talos get --raw=/readyz")
	assert.NotContains(t, calls, "ksail cluster info",
		"an unreachable API must be diagnosed before info can hang and hide its error")
	assert.Contains(
		t,
		calls,
		"ksail cluster delete --provider Kubernetes --name nested-talos --force",
	)
	assert.Contains(t, output, "Talos resources drained")
	assert.NotContains(t, output, "All Kubernetes provider tests passed")
}

func TestNestedReadinessCapturesOnlyEndpointEvidenceBeforeDeletion(t *testing.T) {
	t.Parallel()
	output, calls, err := runNestedCleanup(t, "ready-error", "Talos")
	require.Error(t, err, output)

	diagnosticCall := "kubectl --context fixture-host -n ksail-nested-talos exec dind -c dind -- " +
		"docker inspect --type container --format {{json .NetworkSettings.Ports}} nested-talos-control-plane-1"
	assert.Contains(t, calls, diagnosticCall)
	assert.Contains(t, calls, "get service apiserver")
	assert.Contains(t, calls, "get endpointslices -l kubernetes.io/service-name=apiserver")
	diagnosticIndex := strings.Index(calls, diagnosticCall)
	deleteIndex := strings.Index(calls, "ksail cluster delete")

	require.NotEqual(t, -1, diagnosticIndex)
	require.NotEqual(t, -1, deleteIndex)
	assert.Less(t, diagnosticIndex, deleteIndex)
	assert.NotContains(t, calls, ".Config.Env")
	assert.NotContains(t, calls, "config view")
	assert.NotContains(t, calls, "get secret")
	assert.NotContains(t, output, "All Kubernetes provider tests passed")
}

func TestNestedReadinessBoundsInfoAndStillCleansUp(t *testing.T) {
	t.Parallel()
	output, calls, err := runNestedCleanup(t, "info-hang")
	require.Error(t, err, output)
	assert.Contains(t, calls, "timeout --kill-after=1s 30s ksail cluster info")
	assert.Contains(t, output, "Vanilla info FAILED")
	assert.Contains(
		t,
		calls,
		"ksail cluster delete --provider Kubernetes --name nested-vanilla --force",
	)
	assert.Contains(t, output, "Vanilla resources drained")
	assert.NotContains(t, output, "All Kubernetes provider tests passed")
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

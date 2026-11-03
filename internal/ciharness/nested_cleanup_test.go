package ciharness_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"

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
      network-proof-missing) rm -f "$FIXTURE_LOG_DIR/vanilla-networks.log" ;;
      network-proof-invalid) echo invalid-proof > "$FIXTURE_LOG_DIR/vanilla-networks.log" ;;
    esac
    case "$FIXTURE_MODE" in
      ready-error|ready-evidence-error|ready-backend-error|ready-backend-invalid)
        echo 'fixture nested API unreachable' >&2
        echo fixture-private-address >&2
        exit 1 ;;
      ready-ok-error) echo ok; exit 1 ;;
      ready-hang) echo ok; trap '' TERM; exec sleep 60 ;;
      ready-invalid) echo 'not ready' ;;
      ready-delayed) /bin/sleep 4; echo ok ;;
      ready-flaky)
        if [[ -e "$FIXTURE_CALLS.ready-seen" ]]; then
          echo ok
        else
          : > "$FIXTURE_CALLS.ready-seen"
          echo 'fixture nested API not ready yet' >&2
          exit 1
        fi ;;
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
  '-n ksail-nested-talos'|'-n ksail-nested-vanilla')
    [[ "$FIXTURE_MODE" != ready-evidence-error ]] || { echo fixture-private-error >&2; exit 1; }
    if [[ "$*" == *'docker network inspect bridge kind'* ]]; then
      if [[ "$FIXTURE_MODE" == create-partial ]]; then
        echo 'bridge driver-compatible=true allocation-present=true'
        exit 1
      fi
      if [[ "$*" == *'.Subnet'* ]]; then echo fixture-private-address; else
        echo 'bridge driver-compatible=true allocation-present=true'
        echo 'kind driver-compatible=true allocation-present=true'
      fi
      exit 0
    fi
    case "$*" in
      *custom-columns=*|*'{{json .NetworkSettings.Ports}}'*|*'.Subnet'*|*' ip route')
        echo fixture-private-address
        exit 0 ;;
      *'go-template=service port-present='*) echo 'service port-present=true'; exit 0 ;;
      *'go-template=endpoint-slices present='*) echo 'endpoint-slices present=true'; exit 0 ;;
      *'go-template=pod scheduled='*) echo 'pod scheduled=true address-present=true'; exit 0 ;;
      *'--format container api-binding-present='*) echo 'container api-binding-present=true'; exit 0 ;;
      *'docker exec '*' kubectl --kubeconfig=/etc/kubernetes/admin.conf '*)
        case "$FIXTURE_MODE" in
          ready-backend-error) echo ok; exit 1 ;;
          ready-backend-invalid) echo fixture-private-address; exit 0 ;;
        esac
        echo ok; exit 0 ;;
    esac
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
if [[ "$FIXTURE_MODE" == delete-error || "$FIXTURE_MODE" == info-delete-error ]] && [[ "$2" == delete ]]; then
  echo 'fixture delete failed' >&2
  exit 1
fi
if [[ "$FIXTURE_MODE" == info-delete-error && "$2" == info ]]; then
  echo 'fixture info failed' >&2
  exit 1
fi
if [[ ( "$FIXTURE_MODE" == create-error || "$FIXTURE_MODE" == create-partial ) &&
      "$2" == create && "$*" == *nested-vanilla* ]]; then
  echo 'fixture create failed' >&2
  exit 1
fi
if [[ "$FIXTURE_MODE" == create-hang && "$2" == create ]]; then
  echo 'fixture create stuck' >&2
  trap '' TERM
  exec sleep 60
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
duration="${1:-}"
shift
if [[ "$FIXTURE_MODE" == ready-delayed && "$*" == *--raw=/readyz* ]]; then
  exec "$FIXTURE_TIMEOUT" --kill-after=0.1s "$duration" "$@"
fi
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

	// This bounds the whole fixture, not a product operation. Each real readiness
	// and cleanup deadline is independently exercised by the timeout stub below.
	return runNestedCleanupWithWatchdog(t, false, mode, distributions...)
}

func runNestedCleanupWithWatchdog(
	t *testing.T,
	shortWatchdog bool,
	mode string,
	distributions ...string,
) (string, string, error) {
	t.Helper()

	return runNestedCleanupObserved(t, shortWatchdog, mode, nil, distributions...)
}

func runNestedCleanupObserved(
	t *testing.T,
	shortWatchdog bool,
	mode string,
	observe func(*os.Root),
	distributions ...string,
) (string, string, error) {
	t.Helper()
	requireNestedFixtureBash(t)

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

	command := exec.CommandContext(t.Context(), "timeout", "--kill-after=0.1s", "30s", "bash")
	if filepath.Base(timeoutPath) == "gtimeout" {
		command = exec.CommandContext(t.Context(), "gtimeout", "--kill-after=0.1s", "30s", "bash")
	}

	if shortWatchdog {
		command = exec.CommandContext(t.Context(), "timeout", "--kill-after=0.1s", "5s", "bash")
		if filepath.Base(timeoutPath) == "gtimeout" {
			command = exec.CommandContext(
				t.Context(), "gtimeout", "--kill-after=0.1s", "5s", "bash",
			)
		}

		require.Equal(t, "5s", command.Args[2],
			"the deliberate hang must use the short watchdog, not the normal fixture bound")
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

	if observe != nil {
		observe(root)
	}

	calls, proofErr := nestedFixtureCallsAndProof(t, root, shortWatchdog, mode, err)

	return string(output), calls, errors.Join(err, proofErr)
}

func requireNestedFixtureBash(t *testing.T) {
	t.Helper()
	version, err := exec.CommandContext(t.Context(), "bash", "-c", `printf '%s' "${BASH_VERSINFO[0]}"`).
		Output()
	require.NoError(t, err)

	if string(version) == "3" {
		t.Skip("the Ubuntu composite action requires Bash 4 or newer")
	}
}

func TestNestedBridgeProofPreservesPartialInspection(t *testing.T) {
	t.Parallel()

	var proof []byte

	output, _, err := runNestedCleanupObserved(t, false, "create-partial", func(root *os.Root) {
		proof = readOptionalVersionFixture(t, root, "logs/vanilla-networks.log")
	}, "Vanilla")
	require.Error(t, err, "the create failure must remain a failure: %s", output)
	assert.Equal(t, "bridge driver-compatible=true allocation-present=true\n", string(proof))
}

func TestNestedFixtureRejectsMissingOrInvalidVanillaProof(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ mode, distributions string }{
		{mode: "network-proof-missing"},
		{mode: "network-proof-missing", distributions: "Vanilla"},
		{mode: "network-proof-missing", distributions: "K3s,Vanilla"},
		{mode: "network-proof-invalid", distributions: "Vanilla"},
	} {
		t.Run(testCase.mode+"/"+testCase.distributions, func(t *testing.T) {
			t.Parallel()

			distributions := []string{}
			if testCase.distributions != "" {
				distributions = append(distributions, testCase.distributions)
			}

			output, _, err := runNestedCleanup(t, testCase.mode, distributions...)
			require.ErrorIs(
				t,
				err,
				errNestedNetworkProof,
				"the fixture must reject invalid or absent proof: %s",
				output,
			)
			assert.Contains(
				t,
				output,
				"All Kubernetes provider tests passed",
				"this control must reach proof validation",
			)
		})
	}
}

func TestNestedCompletedShortWatchdogStillRequiresProof(t *testing.T) {
	t.Parallel()

	output, _, err := runNestedCleanupWithWatchdog(t, true, "network-proof-missing", "Vanilla")
	require.ErrorIs(t, err, errNestedNetworkProof, output)
	assert.Contains(t, output, "All Kubernetes provider tests passed")
}

func TestNestedEndpointEvidenceContainsOnlyFixedResults(t *testing.T) {
	t.Parallel()

	for _, distribution := range []string{"Vanilla", "Talos"} {
		t.Run(distribution, func(t *testing.T) {
			t.Parallel()

			var evidence []byte

			output, _, err := runNestedCleanupObserved(
				t,
				false,
				"ready-error",
				func(root *os.Root) {
					evidence = readOptionalVersionFixture(
						t,
						root,
						"logs/"+strings.ToLower(distribution)+"-endpoint.log",
					)
				},
				distribution,
			)
			require.Error(t, err, output)
			assert.NotContains(t, output, "fixture-private")

			want := "service port-present=true\nendpoint-slices present=true\n" +
				"pod scheduled=true address-present=true\ncontainer api-binding-present=true\n"
			if distribution == "Vanilla" {
				want += "backend authenticated-ready=true\n"
			}

			assert.Equal(t, want, string(evidence))
		})
	}
}

func TestNestedEndpointErrorsRemainPrivate(t *testing.T) {
	t.Parallel()

	var evidence []byte

	output, _, err := runNestedCleanupObserved(
		t,
		false,
		"ready-evidence-error",
		func(root *os.Root) {
			evidence = readOptionalVersionFixture(t, root, "logs/vanilla-endpoint.log")
			assert.Equal(t, "network-inspection=unavailable\n",
				string(readOptionalVersionFixture(t, root, "logs/vanilla-networks.log")))
		},
		"Vanilla",
	)
	require.Error(t, err, output)
	assert.NotContains(t, output, "fixture-private-error")
	assert.NotContains(t, string(evidence), "fixture-private-error")
	assert.Equal(t, "service observation=unavailable\nendpoint-slices observation=unavailable\n"+
		"pod observation=unavailable\ncontainer observation=unavailable\n"+
		"backend authenticated-ready=false\n", string(evidence))
}

func TestNestedBackendReadinessRequiresSuccessAndExactOK(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"ready-backend-error", "ready-backend-invalid"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			var evidence []byte

			output, _, err := runNestedCleanupObserved(t, false, mode, func(root *os.Root) {
				evidence = readOptionalVersionFixture(t, root, "logs/vanilla-endpoint.log")
			}, "Vanilla")
			require.Error(t, err, output)
			assert.Contains(t, string(evidence), "backend authenticated-ready=false\n")
			assert.NotContains(t, string(evidence), "backend authenticated-ready=true")
			assert.NotContains(t, output, "fixture-private")
		})
	}
}

func TestNestedEndpointTemplatesDiscardTopologyValues(t *testing.T) {
	t.Parallel()
	action := readCompositeAction(t, ".github/actions/ksail-test-kubernetes-provider/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🧪 Test Kubernetes Provider (nested clusters)")
	pattern := regexp.MustCompile(`-o 'go-template=([^']+)'|--format '(container [^']+)'`)
	matches := pattern.FindAllStringSubmatch(step.Run, -1)
	require.Len(t, matches, 4, "all uploaded endpoint fields must use fixed-output templates")

	for _, match := range matches {
		text := match[1]
		if text == "" {
			text = match[2]
		}

		name := strings.Fields(text)[0]

		for _, present := range []bool{true, false} {
			data := nestedEndpointTemplateData(present)

			parsed, err := template.New("endpoint").Parse(text)
			require.NoError(t, err)

			var output bytes.Buffer
			require.NoError(t, parsed.Execute(&output, data))

			want := map[string]string{
				"service":         "service port-present=%t",
				"endpoint-slices": "endpoint-slices present=%t",
				"pod":             "pod scheduled=%t address-present=%t",
				"container":       "container api-binding-present=%t",
			}

			expected := fmt.Sprintf(want[name], present)
			if name == "pod" {
				expected = fmt.Sprintf(want[name], present, present)
			}

			assert.Equal(t, expected, strings.TrimSpace(output.String()))
			assert.NotContains(t, output.String(), "fixture-private")
		}
	}
}

func nestedEndpointTemplateData(present bool) map[string]any {
	data := map[string]any{
		"spec": map[string]any{}, "status": map[string]any{},
		"NetworkSettings": map[string]any{"Ports": map[string]any{}},
	}
	if !present {
		return data
	}

	data["metadata"] = map[string]string{"name": "fixture-private-name"}
	data["spec"] = map[string]any{
		"ports":    []any{map[string]any{"port": 1, "targetPort": 1, "nodePort": 1}},
		"nodeName": "fixture-private-address",
	}
	data["status"] = map[string]string{
		"podIP":  "fixture-private-address",
		"hostIP": "fixture-private-address",
	}
	data["items"] = []any{map[string]any{
		"metadata": map[string]string{"name": "fixture-private-name"},
		"endpoints": []any{map[string]any{
			"addresses":  []string{"fixture-private-address"},
			"conditions": map[string]bool{"ready": true},
			"nodeName":   "fixture-private-address",
		}},
		"ports": []any{map[string]int{"port": 1}},
	}}
	data["NetworkSettings"] = map[string]any{"Ports": map[string]any{"6443/tcp": []any{
		map[string]string{"HostIP": "fixture-private-address", "HostPort": "fixture-private-port"},
	}}}

	return data
}

func nestedFixtureCallsAndProof(
	t *testing.T, root *os.Root, shortWatchdog bool, mode string, runErr error,
) (string, error) {
	t.Helper()
	calls := string(readOptionalVersionFixture(t, root, "calls"))
	proof := readOptionalVersionFixture(t, root, "logs/vanilla-networks.log")
	assert.NotContains(t, string(proof), "fixture-private-address",
		"the uploaded proof must contain compatibility results, not raw topology")

	// Only the deliberate create hang ends before the capture step in this fixture.
	proofNotReached := shortWatchdog && mode == "create-hang" && runErr != nil
	if !proofNotReached &&
		strings.Contains(strings.ToLower(calls), "ksail cluster create --distribution vanilla") &&
		!validNestedNetworkProof(string(proof)) {
		return calls, errNestedNetworkProof
	}

	return calls, nil
}

var errNestedNetworkProof = errors.New("missing or invalid Vanilla network proof")

func validNestedNetworkProof(proof string) bool {
	if proof == "network-inspection=unavailable\n" {
		return true
	}

	lines := strings.Split(strings.TrimSuffix(proof, "\n"), "\n")
	if len(lines) < 1 || len(lines) > 2 {
		return false
	}

	pattern := regexp.MustCompile(
		`^(bridge|kind) driver-compatible=(true|false) allocation-present=(true|false)$`,
	)
	seen := make(map[string]bool)

	for _, line := range lines {
		match := pattern.FindStringSubmatch(line)
		if match == nil || seen[match[1]] {
			return false
		}

		seen[match[1]] = true
	}

	return true
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

func TestNestedReadinessRetriesATransientFailure(t *testing.T) {
	t.Parallel()

	output, calls, err := runNestedCleanup(t, "ready-flaky")
	require.NoError(t, err, output)
	assert.Contains(t, output, "All Kubernetes provider tests passed")
	assert.NotContains(t, output, "nested API readiness FAILED")
	assert.Equal(
		t,
		2,
		strings.Count("\n"+calls, "\nkubectl --context kind-nested-vanilla get --raw=/readyz"),
		"one failed read must be followed by exactly one retry",
	)
}

func TestNestedReadinessRetainsVanillaEndpointEvidenceBeforeDelete(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"ready-error", "ready-evidence-error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			output, calls, err := runNestedCleanup(t, mode, "Vanilla")
			require.Error(t, err, output)

			deletion := strings.Index(calls, "ksail cluster delete")
			require.NotEqual(t, -1, deletion)

			for _, command := range []string{
				"get service apiserver",
				"get endpointslices",
				"get pod dind",
				"docker inspect --type container",
				"docker exec nested-vanilla-control-plane kubectl " +
					"--kubeconfig=/etc/kubernetes/admin.conf --request-timeout=5s get --raw=/readyz",
			} {
				position := strings.Index(calls, command)
				require.NotEqual(t, -1, position, command)
				assert.Less(t, position, deletion, command)
			}

			assert.Contains(
				t,
				calls,
				"timeout --kill-after=1s 10s kubectl --context fixture-host -n ksail-nested-vanilla",
			)
			assert.Contains(t, output, "Vanilla nested API readiness FAILED")
			assert.Contains(t, output, "Vanilla resources drained")
			assert.NotContains(t, calls, "get secret")
			assert.NotContains(t, calls, ".Config.Env")
			assert.NotContains(t, calls, "docker network inspect kind")
			assert.NotContains(t, calls, " ip route")
		})
	}
}

func TestNestedCreateRetainsVanillaBridgeEvidenceBeforeDelete(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"empty", "create-error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			output, calls, err := runNestedCleanup(t, mode, "Vanilla")
			if mode == "empty" {
				require.NoError(t, err, output)
			} else {
				require.Error(t, err, output)
			}

			inspection := strings.Index(calls, "docker network inspect bridge kind")
			deletion := strings.Index(calls, "ksail cluster delete")

			require.NotEqual(
				t,
				-1,
				inspection,
				"retain allocations even when gated Create refuses success",
			)
			require.NotEqual(t, -1, deletion)
			assert.Less(t, inspection, deletion)
			assert.NotContains(
				t,
				output,
				"fixture endpoint diagnostics",
				"network evidence belongs in the log artifact",
			)
			assert.NotContains(t, calls, "get secret")
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

func TestNestedReadinessAllowsSlowSuccessfulFixture(t *testing.T) {
	t.Parallel()

	output, calls, err := runNestedCleanup(t, "ready-delayed", "Vanilla")
	require.NoError(t, err,
		"a successful readiness response within its own deadline must finish: %s", output)
	assert.Contains(t, calls, "kubectl --context kind-nested-vanilla get --raw=/readyz")
	assert.Contains(t, output, "Vanilla resources drained")
	assert.Contains(t, output, "All Kubernetes provider tests passed")
}

func TestNestedDelayedReadHonorsItsOperationDeadline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeExecutableStub(t, filepath.Join(dir, "kubectl"), nestedCleanupKubectlStub)
	command := exec.CommandContext(t.Context(), "bash", "-s", "--",
		"--kill-after=0.1s", "0.2s", "kubectl", "--context", "kind-nested-vanilla",
		"get", "--raw=/readyz", "--request-timeout=15s")
	command.Stdin = strings.NewReader(nestedCleanupTimeoutStub)

	command.Env = append(nestedCleanupEnvironment(dir, "ready-delayed"),
		"FIXTURE_TIMEOUT="+nestedFixtureTimeoutPath(t))
	output, err := command.CombinedOutput()

	var exitError *exec.ExitError

	require.ErrorAs(t, err, &exitError,
		"the four-second response must exceed the supplied operation deadline: %s", output)
	assert.Equal(t, 124, exitError.ExitCode())
	assert.NotContains(t, string(output), "ok")
}

func TestNestedFixtureWatchdogRejectsAHungCommand(t *testing.T) {
	t.Parallel()

	output, _, err := runNestedCleanupWithWatchdog(t, true, "create-hang", "Vanilla")
	require.Error(t, err, output)
	assert.Contains(t, output, "fixture create stuck",
		"the fixture must reach the deliberately hung command")
	assert.NotContains(t, output, "All Kubernetes provider tests passed")
}

func TestNestedReadinessReportsAPIErrorBeforeInfo(t *testing.T) {
	t.Parallel()
	output, calls, err := runNestedCleanup(t, "ready-error", "Talos")
	require.Error(t, err, output)
	assert.Contains(t, output, "Talos nested API readiness FAILED")
	assert.NotContains(t, output, "fixture nested API unreachable")
	assert.NotContains(t, output, "fixture-private")
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
		"docker inspect --type container"
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

// nestedSummary returns the lifecycle counts the action prints for a trial of
// total distributions, so each case states its whole expected summary once.
func nestedSummary(attempted, passed, failed, unattempted, total int) []string {
	return []string{
		fmt.Sprintf("  Attempted: %d / %d\n", attempted, total),
		fmt.Sprintf("  Passed: %d / %d\n", passed, total),
		fmt.Sprintf("  Failed: %d / %d\n", failed, total),
		fmt.Sprintf("  Unattempted: %d / %d\n", unattempted, total),
	}
}

type nestedSummaryCase struct {
	name          string
	mode          string
	distributions string
	summary       []string
	operations    []string
	failedLine    string
}

func nestedSummaryFailureCases() []nestedSummaryCase {
	return []nestedSummaryCase{
		{
			name:          "delete failure after a healthy create is failed only",
			mode:          "delete-error",
			distributions: "Vanilla",
			summary:       nestedSummary(1, 0, 1, 0, 1),
			operations: []string{
				"Vanilla info and nested API readiness succeeded",
				"Vanilla delete FAILED",
			},
			failedLine: "1 distribution(s) failed (1 failed operation(s))",
		},
		{
			name: "drain failure after a healthy create is failed only", mode: "pending",
			distributions: "Vanilla", summary: nestedSummary(1, 0, 1, 0, 1),
			operations: []string{"Vanilla delete succeeded", "Vanilla cleanup was not verified"},
			failedLine: "1 distribution(s) failed (1 failed operation(s))",
		},
		{
			name: "several failures in one distribution fail it once", mode: "info-delete-error",
			distributions: "Vanilla", summary: nestedSummary(1, 0, 1, 0, 1),
			operations: []string{"Vanilla info FAILED", "Vanilla delete FAILED"},
			failedLine: "1 distribution(s) failed (2 failed operation(s))",
		},
		{
			name:          "a create failure does not hide a later pass",
			mode:          "create-error",
			distributions: "Vanilla,K3s",
			summary:       nestedSummary(2, 1, 1, 0, 2),
			operations: []string{
				"Vanilla create FAILED",
				"Vanilla resources drained",
				"K3s resources drained",
			},
			failedLine: "1 distribution(s) failed (1 failed operation(s))",
		},
		{
			name:          "a drain stop leaves the rest unattempted",
			mode:          "query-error",
			distributions: "Vanilla,K3s,VCluster,Talos",
			summary:       nestedSummary(1, 0, 1, 3, 4),
			operations: []string{
				"Vanilla namespace verification FAILED",
				"Vanilla cleanup was not verified",
			},
			failedLine: "1 distribution(s) failed (1 failed operation(s)); 3 not attempted",
		},
		{
			name: "a host restoration stop leaves the rest unattempted", mode: "host-error",
			distributions: "Vanilla,K3s", summary: nestedSummary(1, 0, 1, 1, 2),
			operations: []string{"Vanilla host context restoration FAILED; stopping nested tests"},
			failedLine: "1 distribution(s) failed (1 failed operation(s)); 1 not attempted",
		},
	}
}

func TestNestedSummaryCountsEachDistributionOnce(t *testing.T) {
	t.Parallel()

	for _, testCase := range nestedSummaryFailureCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			output, _, err := runNestedCleanup(t, testCase.mode, testCase.distributions)
			require.Error(t, err, output)

			for _, line := range testCase.summary {
				assert.Contains(t, output, line)
			}

			for _, operation := range testCase.operations {
				assert.Contains(t, output, operation, "every operation outcome must stay visible")
			}

			assert.Contains(t, output, testCase.failedLine)
			assert.NotContains(t, output, "All Kubernetes provider tests passed")
		})
	}
}

func TestNestedSummaryReportsACompleteHealthyTrial(t *testing.T) {
	t.Parallel()
	output, calls, err := runNestedCleanup(t, "empty", "Vanilla,K3s,VCluster,Talos")
	require.NoError(t, err, output)

	for _, line := range nestedSummary(4, 4, 0, 0, 4) {
		assert.Contains(t, output, line)
	}

	assert.Contains(t, output, "All Kubernetes provider tests passed")
	assert.Equal(t, 4, strings.Count(calls, "ksail cluster delete --provider Kubernetes"))
	assert.Equal(t, 4, strings.Count(output, "resources drained"))
}

package ciharness_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const helmValuesWaitKubectl = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FIXTURE_DIR/calls"
if [[ "$1" == wait ]]; then
  exit 0
fi
[[ "$1" == get ]] || exit 99
if [[ "$2" == helmrelease/values-probe ]]; then
  reads=0
  [[ ! -f "$FIXTURE_DIR/reads" ]] || read -r reads < "$FIXTURE_DIR/reads"
  reads=$((reads + 1))
  printf '%s\n' "$reads" > "$FIXTURE_DIR/reads"
  if [[ "$FIXTURE_MODE" == release-reader-failure ]]; then
    cat "$FIXTURE_DIR/complete.json"
    exit 1
  fi
  if [[ "$FIXTURE_MODE" == malformed-release ]]; then
    printf '{"metadata":'
    exit 0
  fi
  if [[ "$reads" == 1 ]]; then
    cat "$FIXTURE_DIR/pending.json"
    [[ "$FIXTURE_MODE" != transient-release-failure ]] || exit 1
  else
    cat "$FIXTURE_DIR/complete.json"
  fi
else
  [[ "$2" == configmap/values-probe ]] || exit 99
  cat "$FIXTURE_DIR/child.json"
  [[ "$FIXTURE_MODE" != child-reader-failure ]] || exit 1
fi
`

func writeHelmValuesWaitFixture(t *testing.T, dir, mode string) {
	t.Helper()

	expected, complete, child := helmValuesObservationFixture(t)
	_, pending, _ := helmValuesObservationFixture(t)
	mutateHelmValuesRelease(t, pending, "still reconciling")
	helmValuesFixtureMap(t, pending["status"])["observedGeneration"] = -1

	mutateHelmValuesRelease(t, complete, mode)
	mutateHelmValuesChild(t, child, mode)

	for name, value := range map[string]map[string]any{
		"expected.json": expected, "pending.json": pending,
		"complete.json": complete, "child.json": child,
	} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), encoded, 0o600))
	}

	writeExecutableStub(t, filepath.Join(dir, "kubectl"), helmValuesWaitKubectl)
}

func helmValuesWaitCallSite(t *testing.T) string {
	t.Helper()

	source, err := os.ReadFile(helmValuesTrialScript(t))
	require.NoError(t, err)

	script := string(source)
	functions, _, found := strings.Cut(script, `if [[ "${1:-}" == --verify-observation ]]`)
	require.True(t, found)

	start := strings.Index(script, "\tkubectl wait --for=condition=Ready helmrelease/values-probe")
	end := strings.Index(script, "\techo \"Native Flux values confirmed:")

	require.Positive(t, start)
	require.Greater(t, end, start)

	// Exercise the real run_case observation block and its functions without
	// executing any provisioning or mutation. The fixture clock avoids real sleeps.
	return functions + `
directory="$FIXTURE_DIR"
namespace=ksail-values-fixture-cm
target=(--context kind-fixture --kubeconfig "$FIXTURE_DIR/kubeconfig")
sleep() { SECONDS=$((SECONDS + 60)); }
` + script[start:end]
}

func runHelmValuesWaitFixture(t *testing.T, mode string) (string, string, error) {
	t.Helper()

	fixtureRoot, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fixtureRoot.Close()) })

	dir := fixtureRoot.Name()
	writeHelmValuesWaitFixture(t, dir, mode)
	path := filepath.Join(dir, "observe.sh")
	require.NoError(t, fixtureRoot.WriteFile(
		"observe.sh", []byte(helmValuesWaitCallSite(t)), 0o600,
	))

	//nolint:gosec // Fixed repository-owned call site, private fixtures and a fake kubectl only.
	command := exec.CommandContext(t.Context(), "bash", path)

	command.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FIXTURE_DIR="+dir, "FIXTURE_MODE="+mode,
	)
	output, err := command.CombinedOutput()
	calls, readErr := fixtureRoot.ReadFile("calls")
	require.NoError(t, readErr)

	return string(output), string(calls), err
}

func TestHelmValuesWaitForCompleteReconciliation(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"converges", "transient-release-failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			output, calls, err := runHelmValuesWaitFixture(t, mode)
			require.NoError(t, err, output)
			assert.Equal(t, 2, strings.Count(calls, "get helmrelease/values-probe"))
			assert.Contains(t, calls, "--context kind-fixture --kubeconfig ")
			assert.Contains(t, calls, "--request-timeout=10s")
		})
	}
}

func TestHelmValuesWaitRejectsIncompleteOrFailedReadback(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{
		"still reconciling", "stale generation", "other release uid", "wrong type",
		"release-reader-failure", "child-reader-failure", "malformed-release",
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			output, calls, err := runHelmValuesWaitFixture(t, mode)
			require.Error(t, err, output)
			assert.Contains(t, output, "native Flux observation is incomplete or differs")
			assert.LessOrEqual(t, strings.Count(calls, "get helmrelease/values-probe"), 2)
			assert.NotContains(t, calls, "create ")
			assert.NotContains(t, calls, "delete ")
		})
	}
}

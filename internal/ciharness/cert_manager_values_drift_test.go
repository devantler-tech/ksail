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

const certManagerTrialStep = "🧪 cert-manager values drift — one upgrade then no-op"

const certManagerHelmStub = `#!/usr/bin/env bash
set -euo pipefail
printf 'helm %s\n' "$*" >> "$FIXTURE_CALLS"
revision=$(cat "$FIXTURE_REVISION")
case "$1 $2" in
  'version --short') printf 'v3.19.1\n' ;;
  'list --all')
    status=deployed
    [[ "$revision" == 1 || "$revision" == 2 || "$FIXTURE_MODE" != latest_failed ]] || status=failed
    if [[ "$FIXTURE_MODE" == other_release ]]; then
      printf '[{"name":"another-release","status":"deployed","chart":"another-v9.9.9","revision":"1"},\n'
    else
      printf '['
    fi
    printf '{"name":"cert-manager","status":"%s",\n"chart":"cert-manager-v1.21.2","revision":"%s"}]\n' \
      "$status" "$revision"
    [[ "$FIXTURE_MODE" != helm_list_nonzero ]] || exit 55
    ;;
  'get values')
    if [[ "$revision" == 2 || ( "$revision" != 1 && "$FIXTURE_MODE" == wrong_values ) ]]; then
      printf '{"installCRDs":true,"startupapicheck":{"timeout":"12m0s"}}\n'
    else
      printf '{"installCRDs":true,"startupapicheck":{"timeout":"10m0s"}}\n'
    fi
    ;;
  'upgrade cert-manager')
    for flag in '--atomic' '--wait' '--wait-for-jobs' '--reuse-values' '--version v1.21.2' '--set installCRDs=true'; do
      [[ " $* " == *" $flag "* ]] || exit 99
    done
    printf 2 > "$FIXTURE_REVISION"
    ;;
  *) exit 99 ;;
esac
`

const certManagerKubectlStub = `#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >> "$FIXTURE_CALLS"
case "$1" in
  get)
    revision=$(cat "$FIXTURE_REVISION")
    if [[ "$FIXTURE_MODE" == gitops_owned ]]; then
      printf '{"items":[{"metadata":{"labels":{"version":"%s","status":"deployed",\n' "$revision"
      printf '"helm.toolkit.fluxcd.io/name":""}}}]}\n'
    else
      printf '{"items":[{"metadata":{"labels":{"version":"%s","status":"deployed","owner":"helm"}}}]}\n' "$revision"
    fi
    ;;
  rollout)
    [[ "$FIXTURE_MODE" != not_ready ]]
    ;;
  *) exit 99 ;;
esac
`

const certManagerKSailStub = `#!/usr/bin/env bash
set -euo pipefail
printf 'ksail %s\n' "$*" >> "$FIXTURE_CALLS"
validate_config() {
  local config_count=0
  while (( $# > 0 )); do
    if [[ "$1" == --config ]]; then
      [[ $# -ge 2 && "$2" == "$PWD/ksail.yaml" && -f "$2" ]] || exit 64
      config_count=$((config_count + 1))
      shift 2
    else
      shift
    fi
  done
  [[ "$config_count" == 1 ]] || exit 64
}
validate_config "$@"
revision=$(cat "$FIXTURE_REVISION")
empty='"rebootRequired":[],"recreateRequired":[],"rollingRecreate":[],"wipeRequired":[],"unknownBaseline":[]'
drift='{"totalChanges":1,"inPlaceChanges":[{"field":"cluster.certManager.chartValues","category":"in-place"}],'
drift+="$empty}"
noop='{"totalChanges":0,"inPlaceChanges":[],'
noop+="$empty}"
case "$2" in
  diff)
    # Diff reads the saved desired configuration; it has no creation flags.
    shift 2
    while (( $# > 0 )); do
      case "$1" in
        --config|--output|--name|--context|--kubeconfig) shift 2 ;;
        --exit-code|--include-version-drift) shift ;;
        *) echo "unknown flag: $1" >&2; exit 64 ;;
      esac
    done
    if [[ "$FIXTURE_MODE" == missing_drift || "$revision" != 2 ]]; then
      printf '%s\n' "$noop"
    else
      if [[ "$FIXTURE_MODE" == unknown_baseline ]]; then
        drift=$(printf '%s' "$drift" | jq '.unknownBaseline = [{}]')
      fi
      [[ "$FIXTURE_MODE" != two_documents ]] || printf '%s\n' "$drift"
      printf '%s\n' "$drift"
      exit 2
    fi
    ;;
  update)
    [[ " $* " == *' --yes '* && " $* " != *' --force '* && " $* " != *' --force-drain '* ]] || {
      echo 'trial must skip prompts without authorizing destructive drains' >&2; exit 64;
    }
    if [[ "$revision" == 2 ]]; then
      case "$FIXTURE_MODE" in
        no_upgrade) ;;
        two_upgrades) printf 4 > "$FIXTURE_REVISION" ;;
        *) printf 3 > "$FIXTURE_REVISION" ;;
      esac
      printf '%s\n' "$drift"
    else
      if [[ "$FIXTURE_MODE" == repeat_upgrade ]]; then
        printf 4 > "$FIXTURE_REVISION"
      fi
      printf '%s\n' "$noop"
      [[ "$FIXTURE_MODE" == missing_noop_message ]] || echo 'No changes detected' >&2
    fi
    ;;
  *) exit 99 ;;
esac
`

func runCertManagerTrial(t *testing.T, mode string) (string, string, error) {
	t.Helper()
	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, certManagerTrialStep)
	dir := t.TempDir()
	revisionFile := filepath.Join(dir, "revision")
	callsFile := filepath.Join(dir, "calls")

	require.NoError(t, os.WriteFile(revisionFile, []byte("1"), 0o600))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "ksail.yaml"), []byte("fixture: unchanged\n"), 0o600),
	)
	writeExecutableStub(t, filepath.Join(dir, "helm"), certManagerHelmStub)
	writeExecutableStub(t, filepath.Join(dir, "kubectl"), certManagerKubectlStub)
	writeExecutableStub(t, filepath.Join(dir, "ksail"), certManagerKSailStub)

	command := exec.CommandContext(t.Context(), "bash")
	command.Stdin = strings.NewReader(step.Run)
	command.Dir = dir
	command.Env = append(
		os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_WORKSPACE="+repositoryRootForCertTrial(t),
		"DISTRIBUTION=Vanilla", "PROVIDER=Docker",
		"ARGS=--cert-manager Enabled --image-verification Disabled",
		"K8S_VERSION_FLAG=--kubernetes-version v1.36.2",
		"SYSTEM_TEST_LOG_DIR="+dir,
		"FIXTURE_REVISION="+revisionFile,
		"FIXTURE_CALLS="+callsFile,
		"FIXTURE_MODE="+mode,
	)
	output, err := command.CombinedOutput()
	fixtureRoot, openErr := os.OpenRoot(dir)
	require.NoError(t, openErr)
	t.Cleanup(func() { require.NoError(t, fixtureRoot.Close()) })
	calls := readOptionalVersionFixture(t, fixtureRoot, "calls")

	return string(output), string(calls), err
}

func repositoryRootForCertTrial(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	return root
}

func TestCertManagerValuesTrialConverges(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"success", "other_release"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			output, calls, err := runCertManagerTrial(t, mode)
			require.NoError(t, err, output)
			assert.Equal(t, 1, strings.Count(calls, "helm upgrade "), calls)
			assert.Equal(t, 2, strings.Count(calls, "ksail cluster update "), calls)
			assert.Equal(t, 3, strings.Count(calls, "ksail cluster diff "), calls)
			assert.Equal(t, 3, strings.Count(calls, "kubectl rollout status "), calls)
			assert.NotContains(t, calls, "--image-verification")
			assert.Contains(t, calls, "--kubernetes-version v1.36.2")
			assert.Contains(t, output, "one Helm upgrade and a repeated no-op")
		})
	}
}

func TestCertManagerValuesTrialRejectsIncompleteProof(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ mode, want string }{
		{mode: "missing_drift", want: "values drift did not exit exactly 2"},
		{mode: "no_upgrade", want: "reconciliation did not create exactly one revision"},
		{mode: "two_upgrades", want: "reconciliation did not create exactly one revision"},
		{mode: "wrong_values", want: "reconciliation did not restore the desired values"},
		{mode: "gitops_owned", want: "latest storage revision differs or is GitOps-owned"},
		{mode: "not_ready", want: "cert-manager is not ready"},
		{mode: "repeat_upgrade", want: "repeated update changed the Helm revision"},
		{mode: "missing_noop_message", want: "repeated update omitted its no-op result"},
		{mode: "unknown_baseline", want: "expected only one in-place cert-manager values change"},
		{mode: "two_documents", want: "expected only one in-place cert-manager values change"},
		{mode: "latest_failed", want: "latest release is not a single deployed revision"},
		{mode: "helm_list_nonzero", want: "could not read latest Helm release"},
	} {
		t.Run(testCase.mode, func(t *testing.T) {
			t.Parallel()
			output, _, err := runCertManagerTrial(t, testCase.mode)
			require.Error(t, err, output)
			assert.Contains(t, output, testCase.want)
		})
	}
}

func TestCertManagerValuesTrialUsesExistingRealNodeLeg(t *testing.T) {
	t.Parallel()
	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, certManagerTrialStep)
	assert.Contains(t, step.If, "inputs.provider == 'Docker'")
	assert.Contains(t, step.If, "inputs.distribution == 'Vanilla'")
	assert.Contains(t, step.If, "contains(inputs.args, '--cert-manager Enabled')")
	assert.Equal(t, "${{ steps.resolve-args.outputs.args }}", step.Env["ARGS"])
	assert.Equal(t, "${{ steps.k8s-version-pin.outputs.flag }}", step.Env["K8S_VERSION_FLAG"])
}

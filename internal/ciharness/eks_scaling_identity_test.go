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

const eksScalingReaderFixture = `#!/usr/bin/env bash
set -euo pipefail
printf '%s %s\n' "${0##*/}" "$*" >> "$FIXTURE_DIR/calls"
updates=0
[[ ! -f "$FIXTURE_DIR/updates" ]] || read -r updates < "$FIXTURE_DIR/updates"
desired=1
[[ "$updates" != 1 ]] || desired=2
case "${0##*/}" in
  aws)
    if [[ "$*" == *describe-cluster* ]]; then
      printf 'stable-cluster-arn\t2026-10-01T12:00:00Z\n'
    elif [[ "$*" == *describe-nodegroup* ]]; then
      [[ "$*" == *'--nodegroup-name primary'* ]] || exit 99
      [[ "$FIXTURE_MODE" != reader-failure ]] || exit 1
      if [[ "$FIXTURE_MODE" == malformed ]]; then printf '{'; exit 0; fi
      if [[ "$FIXTURE_MODE" == empty ]]; then printf '[null,null]'; exit 0; fi
      created='"2026-10-01T12:00:00Z"'
      [[ "$FIXTURE_MODE" != epoch ]] || created=1790856000
      group=primary
      [[ "$FIXTURE_MODE" != wrong-group ]] || group=unrelated
      id=original
      if [[ "$FIXTURE_MODE" == replaced-up && "$updates" -ge 1 ]] ||
         [[ "$FIXTURE_MODE" == replaced-down && "$updates" -ge 2 ]]; then
        id=replacement
        created='"2026-10-05T12:00:00Z"'
      fi
      printf '["arn:aws:eks:us-east-1:123456789012:nodegroup/fixture/%s/%s",%s]\n' "$group" "$id" "$created"
    else exit 99; fi ;;
  ksail)
    [[ "$*" == 'cluster update --yes' ]] || exit 99
    printf '%s\n' "$((updates + 1))" > "$FIXTURE_DIR/updates" ;;
  eksctl)
    [[ "$*" == *'get nodegroup'* ]] || exit 99
    printf '[{"Status":"ACTIVE","DesiredCapacity":%s,"MinSize":1,"MaxSize":%s}]\n' "$desired" "$desired" ;;
  kubectl)
    [[ "$*" == 'get nodes -o json' ]] || exit 99
    jq -n --argjson count "$desired" \
      '{items: [range($count) | {status:{conditions:[{type:"Ready",status:"True"}]}}]}' ;;
  yq)
    if [[ "$1" == -i ]]; then exit 0; fi
    if [[ "$*" == *'.name'* ]]; then printf 'primary\n'
    elif [[ "$*" == *length* ]]; then printf '1\n'
    else exit 99; fi ;;
  *) exit 99 ;;
esac
`

func makeEKSSmokeScalingFixture(t *testing.T, script, mode string) (*exec.Cmd, string) {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"aws", "ksail", "eksctl", "kubectl", "yq"} {
		writeExecutableStub(t, filepath.Join(dir, name), eksScalingReaderFixture)
	}

	path := filepath.Join(dir, "scale.sh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))

	//nolint:gosec // Fixed repository-owned workflow and private provider-free fixtures.
	command := exec.CommandContext(t.Context(), "bash", path)

	command.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FIXTURE_DIR="+dir, "FIXTURE_MODE="+mode, "KSAIL_EKS_WORKDIR="+dir,
		"KSAIL_EKS_CLUSTER_NAME=fixture", "AWS_REGION=us-east-1",
	)

	return command, dir
}

func TestEKSSmokeRejectsReplacedManagedNodeGroups(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/system-test-eks.yaml")
	step := findHarnessStep(
		t,
		workflow.Jobs["smoke-test"].Steps,
		"🧪 ksail cluster update scales EKS nodes",
	)

	modes := []string{
		"unchanged", "epoch", "replaced-up", "replaced-down",
		"empty", "malformed", "reader-failure", "wrong-group",
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			command, dir := makeEKSSmokeScalingFixture(t, step.Run, mode)

			output, err := command.CombinedOutput()
			if mode == "unchanged" || mode == "epoch" {
				require.NoError(t, err, string(output))
			} else {
				require.Error(t, err, "unsafe scaling observation was accepted: %s", output)
			}

			fixtureRoot, rootErr := os.OpenRoot(dir)
			require.NoError(t, rootErr)
			t.Cleanup(func() { assert.NoError(t, fixtureRoot.Close()) })

			calls, readErr := fixtureRoot.ReadFile("calls")
			require.NoError(t, readErr)

			switch mode {
			case "unchanged", "epoch":
				assert.Equal(t, 2, strings.Count(string(calls), "ksail cluster update --yes"))
				assert.Equal(t, 3, strings.Count(string(calls), "aws eks describe-nodegroup"))
			case "replaced-up":
				assert.Equal(t, 1, strings.Count(string(calls), "ksail cluster update --yes"))
				assert.Contains(t, string(output), "managed node group identity changed")
			case "replaced-down":
				assert.Equal(t, 2, strings.Count(string(calls), "ksail cluster update --yes"))
				assert.Contains(t, string(output), "managed node group identity changed")
			default:
				assert.NotContains(t, string(calls), "ksail cluster update")
			}

			assert.NotContains(t, string(calls), "delete")
		})
	}
}

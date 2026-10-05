package ciharness_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAutoscalerAuditTrialRejectsFalseAcceptance(t *testing.T) {
	t.Parallel()

	modes := []string{
		"complete",
		"foreign-reported",
		"retry-success",
		"mutated",
		"cleanup-failed",
		"cleanup-retained",
		"cancelled",
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			state, bin := prepareAutoscalerAuditFixture(t)

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			cmd := autoscalerAuditTrialCommand(t, ctx, state, bin, mode)
			output, err := cmd.CombinedOutput()
			assert.NotContains(t, string(output), "must-never-reach-evidence")

			if mode == "complete" {
				assertAutoscalerAuditAccepted(t, state, output, err)
			} else {
				require.Error(t, err, string(output))
				assert.NotContains(t, string(output), "PASS:")

				if mode == "cancelled" {
					_, statErr := os.Stat(filepath.Join(state, "deleted"))
					require.NoError(t, statErr, "termination must still remove the owned probe")
				}
			}
		})
	}
}

func autoscalerAuditTrialCommand(
	t *testing.T, ctx context.Context, state, bin, mode string,
) *exec.Cmd {
	t.Helper()

	script, pathErr := filepath.Abs(
		filepath.Join("..", "..", ".github/actions/ksail-system-test/autoscaler-audit-trial.sh"),
	)
	require.NoError(t, pathErr)

	//nolint:gosec // Executes the repository-owned trial entry point with fixed fixture inputs.
	cmd := exec.CommandContext(ctx, script)
	cmd.Dir = state
	require.NoError(t, os.WriteFile(filepath.Join(state, "ksail.yaml"), []byte("unchanged"), 0o600))
	cmd.Env = append(
		os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STATE="+state,
		"MODE="+mode,
		"GITHUB_RUN_ID=1234",
		"CLUSTER_NAME=st-hetzner-dispatch-1234",
		"K8S_VERSION=v1.36.2",
		"HCLOUD_TOKEN=fixture",
		"EVIDENCE_DIR="+filepath.Join(state, "evidence"),
	)

	return cmd
}

func assertAutoscalerAuditAccepted(t *testing.T, state string, output []byte, trialErr error) {
	t.Helper()
	require.NoError(t, trialErr, string(output))
	assert.Contains(
		t,
		string(output),
		"PASS: foreign network excluded; both unchanged updates failed; server preserved; probe absent",
	)
	//nolint:gosec // Reads a call record written by the test's stub into its own temporary directory.
	calls, readErr := os.ReadFile(filepath.Join(state, "ksail-calls"))
	require.NoError(t, readErr)
	assert.Equal(t, 3, strings.Count(strings.TrimSpace(string(calls)), "\n")+1)
	assert.NotContains(t, string(calls), "cluster delete")
}

func prepareAutoscalerAuditFixture(t *testing.T) (string, string) {
	t.Helper()

	state, bin := t.TempDir(), t.TempDir()
	writeExecutable(t, filepath.Join(bin, "timeout"), "#!/usr/bin/env bash\nshift 2\nexec \"$@\"\n")
	writeExecutable(t, filepath.Join(bin, "hcloud"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$STATE/hcloud-calls"
case "$1 $2" in
"server list") [[ ! -f "$STATE/created" || -f "$STATE/deleted" ]] || echo 42 ;;
"server create") touch "$STATE/created"; echo '{"server":{"id":42},"root_password":"must-never-reach-evidence"}' ;;
"server attach-to-network") touch "$STATE/attached" ;;
"server describe")
  status=off; [[ ! -f "$STATE/mutated" ]] || status=running
  jq -cn --arg name "$CLUSTER_NAME-audit-removed" --arg run "$GITHUB_RUN_ID" --arg status "$status" \
    '{id:42,name:$name,created:"2026-10-05T00:00:00Z",status:$status,server_type:{id:1},image:{id:2},
      labels:{"ksail.trial.run":$run,"hcloud/node-group":"removed-pool"}}'
  ;;
"server delete")
  [[ "$MODE" != cleanup-failed ]] || exit 1
  [[ "$MODE" == cleanup-retained ]] || touch "$STATE/deleted"
  ;;
*) echo 'unexpected hcloud operation' >&2; exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(bin, "ksail"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$STATE/ksail-calls"
count=0; [[ ! -f "$STATE/count" ]] || count=$(cat "$STATE/count")
count=$((count+1)); echo "$count" > "$STATE/count"
if [[ "$MODE" == cancelled && "$count" -eq 2 ]]; then
  kill -TERM "$PPID"
  exit 143
fi
if [[ ! -f "$STATE/attached" && "$MODE" != foreign-reported ]] || \
   [[ "$count" -eq 3 && "$MODE" == retry-success ]]; then
  echo 'No changes detected'; exit 0
fi
[[ "$MODE" != mutated ]] || touch "$STATE/mutated"
echo "Autoscaler node $CLUSTER_NAME-audit-removed is left untouched: node autoscaler is disabled"
echo '1 changes failed to apply:'
exit 1
`)

	return state, bin
}

func TestAutoscalerAuditTrialAdmitsOnlyBoundedDispatch(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow),
	)
	input, found := workflow.On.WorkflowDispatch.Inputs["autoscaler_inventory_trial"]
	require.True(t, found)
	assert.Equal(t, false, input.Default)

	steps := workflow.Jobs["system-test"].Steps
	guard := findHarnessStep(t, steps, "✅ Validate bounded autoscaler inventory trial")
	assertAutoscalerAdmissionPrecedesProvisioning(t, steps, guard.Name)

	for _, testCase := range []struct {
		name, distribution, init, cp, workers, resize string
		accepted                                      bool
	}{
		{"one Talos node", "Talos", "true", "1", "0", "", true},
		{"direct provider", "K3s", "true", "1", "0", "", false},
		{"no init", "Talos", "false", "1", "0", "", false},
		{"extra control planes", "Talos", "true", "3", "0", "", false},
		{"extra workers", "Talos", "true", "1", "1", "", false},
		{"rolling replacement", "Talos", "true", "1", "0", "cx33", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "bash", "-c", guard.Run) //nolint:gosec

			cmd.Env = append(
				os.Environ(),
				"DISTRIBUTION="+testCase.distribution,
				"INIT="+testCase.init,
				"CONTROL_PLANES="+testCase.cp,
				"WORKERS="+testCase.workers,
				"ROLLING_RESIZE="+testCase.resize,
			)
			output, err := cmd.CombinedOutput()

			if testCase.accepted {
				require.NoError(t, err, string(output))
			} else {
				require.Error(t, err, string(output))
			}
		})
	}
}

func assertAutoscalerAdmissionPrecedesProvisioning(
	t *testing.T,
	steps []harnessStep,
	guardName string,
) {
	t.Helper()

	guardIndex := -1

	for index, step := range steps {
		if step.Name == guardName {
			guardIndex = index
		}

		if strings.Contains(step.Uses, "/ksail-cluster") ||
			strings.Contains(step.Uses, "/ksail-system-test") {
			require.GreaterOrEqual(
				t,
				guardIndex,
				0,
				"admission must precede every provisioning action",
			)
			require.Less(t, guardIndex, index)
		}
	}
}

func TestAutoscalerAuditTrialRejectsExistingRunResources(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow),
	)
	guard := findHarnessStep(t, workflow.Jobs["system-test"].Steps,
		"✅ Verify inventory trial starts without run-owned resources")
	assert.Equal(t, 3, guard.TimeoutMinutes)
	assert.Contains(t, guard.If, "inputs.autoscaler_inventory_trial")

	for _, mode := range []string{"empty", "server", "floating-ip", "placement-group", "firewall", "network", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			bin := t.TempDir()
			writeExecutable(
				t,
				filepath.Join(bin, "timeout"),
				"#!/usr/bin/env bash\nshift 2\nexec \"$@\"\n",
			)
			writeExecutable(t, filepath.Join(bin, "hcloud"), `#!/usr/bin/env bash
set -euo pipefail
[[ "$2" == list && "$*" == *'ksail.cluster.name=st-hetzner-dispatch-1234'* ]] || exit 2
[[ "$MODE" != unknown ]] || exit 1
[[ "$1" != "$MODE" ]] || echo 42
`)

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			command := exec.CommandContext(ctx, "bash", "-c", guard.Run) //nolint:gosec

			command.Env = append(
				os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"MODE="+mode,
				"GITHUB_RUN_ID=1234",
				"HCLOUD_TOKEN=fixture",
			)
			output, err := command.CombinedOutput()

			if mode == "empty" {
				require.NoError(t, err, string(output))
			} else {
				require.Error(t, err, string(output))
			}
		})
	}
}

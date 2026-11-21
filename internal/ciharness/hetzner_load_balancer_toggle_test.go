package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const loadBalancerToggleStep = "🧪 ksail cluster update — load balancers off then on"

// ksail#7678: a Hetzner dispatch can switch load balancers off and back on with cluster
// update. The option is off by default and passed to the system-test action.
func TestHetznerLoadBalancerToggleIsOptIn(t *testing.T) {
	t.Parallel()

	// Only workflow_dispatch is decoded: the schedule trigger is a list.
	//
	//nolint:tagliatelle // GitHub Actions defines this external key in snake_case.
	var trigger struct {
		On struct {
			WorkflowDispatch struct {
				Inputs map[string]struct {
					Type    string `yaml:"type"`
					Default any    `yaml:"default"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
		} `yaml:"on"`
	}
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &trigger),
	)

	input := trigger.On.WorkflowDispatch.Inputs["load_balancer_toggle"]
	require.Equal(t, "boolean", input.Type)
	require.Equal(t, false, input.Default)

	workflow := readCIWorkflow(t, ".github/workflows/system-test-hetzner.yaml")
	system := findHarnessStep(t, workflow.Jobs["system-test"].Steps, "🧪 Run KSail System Test")
	assert.Equal(
		t,
		"${{ github.event_name == 'workflow_dispatch' && inputs.load_balancer_toggle || false }}",
		stringValue(system.With["load-balancer-toggle"]),
	)

	validate := findHarnessStep(
		t,
		workflow.Jobs["system-test"].Steps,
		"✅ Validate direct-provider smoke selection",
	)
	log, err := runStepScript(t, validate.Run, "", []string{
		"DISTRIBUTION=Vanilla", "LOAD_BALANCER_TOGGLE=true",
	})
	require.Error(t, err, log)
	assert.Contains(t, log, "load_balancer_toggle=true")
}

// The toggle edits ksail.yaml, which only an initialized project has, and needs load
// balancers on at create, so a dispatch that cannot run it fails before any server exists.
func TestLoadBalancerToggleNeedsInitializedTalosWithLoadBalancers(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/system-test-hetzner.yaml")
	validate := findHarnessStep(
		t,
		workflow.Jobs["system-test"].Steps,
		"✅ Validate load-balancer toggle trial",
	)
	// Without this guard the check would fail every scheduled smoke before a cluster exists.
	assert.Equal(t, "inputs.load_balancer_toggle", validate.If)

	for _, testCase := range []struct {
		name string
		env  []string
		ok   bool
	}{
		{"initialized Talos default", []string{"DISTRIBUTION=Talos", "INIT=true", "LOAD_BALANCER=Default"}, true},
		{"initialized Talos enabled", []string{"DISTRIBUTION=Talos", "INIT=true", "LOAD_BALANCER=Enabled"}, true},
		{"no init", []string{"DISTRIBUTION=Talos", "INIT=false", "LOAD_BALANCER=Default"}, false},
		{"disabled at create", []string{"DISTRIBUTION=Talos", "INIT=true", "LOAD_BALANCER=Disabled"}, false},
		{"not Talos", []string{"DISTRIBUTION=K3s", "INIT=true", "LOAD_BALANCER=Default"}, false},
	} {
		log, err := runStepScript(t, validate.Run, "", testCase.env)
		if testCase.ok {
			require.NoError(t, err, "%s: %s", testCase.name, log)
		} else {
			require.Error(t, err, "%s: %s", testCase.name, log)
			assert.Contains(t, log, "load-balancer toggle trial requires", testCase.name)
		}
	}
}

// The toggle checks the running cloud controller in both directions, and a failure in it
// collects the same diagnostics as the other update trials.
func TestLoadBalancerToggleChecksBothDirections(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	require.Contains(t, action.Inputs, "load-balancer-toggle")
	assert.Equal(t, "false", action.Inputs["load-balancer-toggle"].Default)

	step := findHarnessStep(t, action.Runs.Steps, loadBalancerToggleStep)
	assert.Contains(t, step.If, "inputs.load-balancer-toggle == 'true'")
	assert.Contains(t, step.If, "inputs.provider == 'Hetzner'")
	assert.Contains(t, step.If, "inputs.distribution == 'Talos'")
	assert.Contains(t, step.If, "inputs.init == 'true'")
	// An explicit --load-balancer flag would override ksail.yaml, which drives the toggle.
	assert.Contains(t, step.Run, `-e 's/--load-balancer[ =][^ ]*//'`)

	for _, want := range []string{
		`.spec.cluster.loadBalancer = "Disabled"`,
		"load balancers are disabled",
		`"--controllers=*,-service"`,
		"kubectl wait --for=condition=Ready nodes --all",
		"dry_run_unchanged cluster-update-load-balancer-disable-dry-run",
		"dry_run_unchanged cluster-update-load-balancer-enable-dry-run",
		"strenv(ORIGINAL)",
	} {
		assert.Contains(t, step.Run, want)
	}

	debug := findHarnessStep(t, action.Runs.Steps, "🐞 Debug Kubernetes failure")
	assert.Contains(t, debug.If, "steps.cluster-update-load-balancer-toggle.outcome == 'failure'")
}

// The toggle waits on the controller's own workload kind, so a stuck Deployment rollout
// reports its own failure instead of falling through to a missing DaemonSet.
func TestLoadBalancerToggleWaitsOnTheControllerKind(t *testing.T) {
	t.Parallel()

	jqPath, err := exec.LookPath("jq")
	require.NoError(t, err, "the toggle step requires jq")

	waitCCM := toggleWaitCCM(t)

	for _, testCase := range []struct {
		name, kinds, rollout string
		ok                   bool
		want                 []string
	}{
		{"deployment ready", "Deployment", "0", true, []string{"rollout status deployment/hcloud-cloud-controller-manager"}},
		{"daemonset ready", "DaemonSet", "0", true, []string{"rollout status daemonset/hcloud-cloud-controller-manager"}},
		{"deployment stuck", "Deployment", "1", false, []string{"deployment rollout timed out"}},
		{"missing", "", "0", false, []string{"expected exactly one hcloud-cloud-controller-manager"}},
		{"both kinds", "Deployment,DaemonSet", "0", false, []string{"expected exactly one hcloud-cloud-controller-manager"}},
	} {
		fakeDir := writeFakeCCMKubectl(t, jqPath, testCase.kinds, testCase.rollout)

		log, err := runStepScript(t, waitCCM+"wait_ccm\n", fakeDir, nil)
		if testCase.ok {
			require.NoError(t, err, "%s: %s", testCase.name, log)
		} else {
			require.Error(t, err, "%s: %s", testCase.name, log)
		}

		for _, want := range testCase.want {
			assert.Contains(t, log, want, testCase.name)
		}

		if testCase.kinds == "Deployment" {
			assert.NotContains(
				t,
				log,
				"rollout status daemonset",
				"%s: never falls through",
				testCase.name,
			)
		}
	}
}

// toggleWaitCCM returns the toggle step's wait_ccm function definition.
func toggleWaitCCM(t *testing.T) string {
	t.Helper()

	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, loadBalancerToggleStep)
	start := strings.Index(step.Run, "wait_ccm() {")
	require.NotEqual(t, -1, start, "toggle step defines wait_ccm")
	end := strings.Index(step.Run[start:], "\n}\n")
	require.NotEqual(t, -1, end, "wait_ccm has a closing brace")

	return step.Run[start : start+end+len("\n}\n")]
}

// writeFakeCCMKubectl writes a kubectl that lists the given controller kinds and reports each
// rollout wait, failing it when rollout is not 0.
func writeFakeCCMKubectl(t *testing.T, jqPath, kinds, rollout string) string {
	t.Helper()

	fakeDir := t.TempDir()
	require.NoError(t, os.Symlink(jqPath, filepath.Join(fakeDir, "jq")))

	kubectl := `#!/bin/bash
if [ "$3" = get ]; then
  printf '{"items":['
  sep=""
  for kind in ` + strings.ReplaceAll(kinds, ",", " ") + `; do
    printf '%s{"kind":"%s","metadata":{"name":"hcloud-cloud-controller-manager"}}' "$sep" "$kind"
    sep=","
  done
  printf ']}'
  exit 0
fi
echo "kubectl $*"
if [ "` + rollout + `" != 0 ]; then echo "deployment rollout timed out"; exit 1; fi
`
	//nolint:gosec // the fake must be executable
	require.NoError(t, os.WriteFile(filepath.Join(fakeDir, "kubectl"), []byte(kubectl), 0o700))

	return fakeDir
}

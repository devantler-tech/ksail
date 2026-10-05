package ciharness_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const helmValuesPrecedenceStep = "🧪 Native Flux Helm values precedence"

func TestHelmValuesPrecedenceTrialUsesExistingNamedFluxCluster(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, helmValuesPrecedenceStep)
	assert.Contains(t, step.If, "inputs.provider == 'Docker'")
	assert.Contains(t, step.If, "inputs.distribution == 'Vanilla'")
	assert.Contains(t, step.If, "inputs.init == 'true'")
	assert.Contains(t, step.If, "--gitops-engine Flux")
	assert.Contains(t, step.If, "--name system-test-cluster")
	assert.Equal(t, "${{ steps.resolve-args.outputs.args }}", step.Env["ARGS"])
	assert.Contains(t, step.Run, "helm-values-precedence.sh")
	assert.NotContains(t, step.Run, "cluster create")
}

func helmValuesObservationFixture(t *testing.T) (map[string]any, map[string]any, map[string]any) {
	t.Helper()

	values := map[string]any{
		"replicaCount": "4",
		"nested":       map[string]any{"flag": false, "zero": 0, "empty": "", "list": []any{}},
		"payload":      "{\"token\":\"a,b=c\",\"path\":\"C:\\\\tmp\"}\nsecond=line",
	}
	encodedValues, err := json.Marshal(values)
	require.NoError(t, err)

	expected := map[string]any{
		"namespace": "ksail-values-fixture-cm", "release": "values-probe",
		"uid": "fixture-release-uid", "generation": 2, "values": values,
	}
	release := map[string]any{
		"metadata": map[string]any{
			"name": "values-probe", "namespace": "ksail-values-fixture-cm",
			"uid": "fixture-release-uid", "generation": 2,
		},
		"status": map[string]any{
			"observedGeneration": 2,
			"conditions": []any{map[string]any{
				"type": "Ready", "status": "True", "observedGeneration": 2,
			}},
		},
	}
	child := map[string]any{
		"metadata": map[string]any{
			"name": "values-probe", "namespace": "ksail-values-fixture-cm",
			"annotations": map[string]any{
				"meta.helm.sh/release-name":      "values-probe",
				"meta.helm.sh/release-namespace": "ksail-values-fixture-cm",
			},
		},
		"data": map[string]any{"values.json": string(encodedValues)},
	}

	return expected, release, child
}

func helmValuesFixtureMap(t *testing.T, value any) map[string]any {
	t.Helper()

	object, ok := value.(map[string]any)
	require.True(t, ok, "expected an object in the owned JSON fixture")

	return object
}

func mutateHelmValuesRelease(t *testing.T, release map[string]any, mutation string) {
	t.Helper()

	metadata := helmValuesFixtureMap(t, release["metadata"])
	status := helmValuesFixtureMap(t, release["status"])
	conditions, ok := status["conditions"].([]any)
	require.True(t, ok, "expected conditions in the owned release fixture")
	require.NotEmpty(t, conditions)

	condition := helmValuesFixtureMap(t, conditions[0])

	switch mutation {
	case "stale generation":
		status["observedGeneration"] = 1
	case "stale ready condition":
		condition["observedGeneration"] = 1
	case "not ready":
		condition["status"] = "False"
	case "no conditions":
		delete(status, "conditions")
	case "controller error":
		status["conditions"] = append(conditions, map[string]any{
			"type": "Stalled", "status": "True", "observedGeneration": 2,
		})
	case "still reconciling":
		status["conditions"] = append(conditions, map[string]any{
			"type": "Reconciling", "status": "True", "observedGeneration": 2,
		})
	case "other release uid":
		metadata["uid"] = "unrelated-release"
	}
}

func mutateHelmValuesChild(t *testing.T, child map[string]any, mutation string) {
	t.Helper()

	metadata := helmValuesFixtureMap(t, child["metadata"])
	annotations := helmValuesFixtureMap(t, metadata["annotations"])
	data := helmValuesFixtureMap(t, child["data"])

	switch mutation {
	case "other namespace":
		metadata["namespace"] = "unrelated"
	case "other Helm owner":
		annotations["meta.helm.sh/release-name"] = "unrelated"
	case "missing values":
		delete(data, "values.json")
	case "wrong type":
		data["values.json"] = `{"replicaCount":4}`
	case "partial readback":
		data["values.json"] = `{"replicaCount":`
	}
}

func helmValuesTrialScript(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	return filepath.Join(
		root,
		".github",
		"actions",
		"ksail-system-test",
		"helm-values-precedence.sh",
	)
}

func runHelmValuesObservation(t *testing.T, mutation string) (string, error) {
	t.Helper()

	expected, release, child := helmValuesObservationFixture(t)
	mutateHelmValuesRelease(t, release, mutation)
	mutateHelmValuesChild(t, child, mutation)

	dir := t.TempDir()
	paths := make([]string, 0, 3)

	for index, value := range []map[string]any{expected, release, child} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)

		if mutation == "two JSON documents" && index == 2 {
			encoded = append(append(encoded, '\n'), encoded...)
		}

		path := filepath.Join(dir, []string{"expected.json", "release.json", "child.json"}[index])
		require.NoError(t, os.WriteFile(path, encoded, 0o600))
		paths = append(paths, path)
	}

	args := append([]string{helmValuesTrialScript(t), "--verify-observation"}, paths...)
	//nolint:gosec // Runs a fixed repository-owned script with test-owned JSON paths.
	command := exec.CommandContext(t.Context(), "bash", args...)
	output, err := command.CombinedOutput()

	return string(output), err
}

func TestHelmValuesPrecedenceObservationAcceptsCompleteTypedReadback(t *testing.T) {
	t.Parallel()

	output, err := runHelmValuesObservation(t, "complete")
	require.NoError(t, err, output)
}

func TestHelmValuesPrecedenceObservationRejectsIncompleteEvidence(t *testing.T) {
	t.Parallel()

	for _, mutation := range []string{
		"stale generation", "stale ready condition", "not ready", "no conditions",
		"controller error", "still reconciling", "other release uid", "other namespace",
		"other Helm owner", "missing values", "wrong type", "partial readback", "two JSON documents",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()

			output, err := runHelmValuesObservation(t, mutation)
			require.Error(t, err, output)
			assert.Contains(t, output, "native Flux observation is incomplete or differs")
		})
	}
}

func helmValuesTargetData(mismatch string) (map[string]any, map[string]any, string) {
	args := "--name fixture --gitops-engine Flux"
	context := "kind-fixture"
	server := "https://127.0.0.1:16443"
	cluster := "fixture"

	switch mismatch {
	case "missing name":
		args = "--gitops-engine Flux"
	case "other context":
		context = "kind-unrelated"
	case "other Docker cluster":
		cluster = "unrelated"
	case "other API port":
		server = "https://127.0.0.1:26443"
	}

	config := map[string]any{
		"current-context": context,
		"contexts": []any{map[string]any{
			"name": "kind-fixture", "context": map[string]any{"cluster": "kind-fixture"},
		}},
		"clusters": []any{map[string]any{
			"name": "kind-fixture", "cluster": map[string]any{"server": server},
		}},
	}
	container := map[string]any{
		"State": map[string]any{"Running": true},
		"Config": map[string]any{"Labels": map[string]any{
			"io.x-k8s.kind.cluster": cluster, "io.x-k8s.kind.role": "control-plane",
		}},
		"NetworkSettings": map[string]any{"Ports": map[string]any{
			"6443/tcp": []any{map[string]any{"HostIp": "127.0.0.1", "HostPort": "16443"}},
		}},
	}

	return config, container, args
}

func writeHelmValuesTargetFixture(t *testing.T, mismatch string) (*os.Root, string) {
	t.Helper()

	fixtureRoot, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fixtureRoot.Close()) })

	config, container, args := helmValuesTargetData(mismatch)

	for name, value := range map[string]any{"target.json": config, "container.json": []any{container}} {
		encoded, encodeErr := json.Marshal(value)
		require.NoError(t, encodeErr)
		require.NoError(t, fixtureRoot.WriteFile(name, encoded, 0o600))
	}

	require.NoError(t, fixtureRoot.WriteFile("kubeconfig", []byte("private fixture"), 0o600))
	require.NoError(t, fixtureRoot.WriteFile("calls", nil, 0o600))
	writeExecutableStub(t, filepath.Join(fixtureRoot.Name(), "kubectl"), `#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >> "$FIXTURE_CALLS"
[[ "$1 $2" == 'config view' ]] || exit 99
cat "$FIXTURE_TARGET"
`)
	writeExecutableStub(t, filepath.Join(fixtureRoot.Name(), "docker"), `#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >> "$FIXTURE_CALLS"
if [[ "$1 $2" == 'context inspect' ]]; then
  printf '%s\n' "$FIXTURE_DOCKER_ENDPOINT"
  exit 0
fi
[[ "$1 $2" == 'inspect fixture-control-plane' ]] || exit 99
cat "$FIXTURE_CONTAINER"
`)
	writeExecutableStub(
		t,
		filepath.Join(fixtureRoot.Name(), "helm"),
		"#!/usr/bin/env bash\nexit 99\n",
	)

	return fixtureRoot, args
}

func runHelmValuesTargetFixture(t *testing.T, mismatch string) (string, string, error) {
	t.Helper()

	fixtureRoot, args := writeHelmValuesTargetFixture(t, mismatch)
	dir := fixtureRoot.Name()
	//nolint:gosec // Runs the fixed repository-owned trial against private executable/JSON fixtures.
	command := exec.CommandContext(t.Context(), "bash", helmValuesTrialScript(t))

	command.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"DISTRIBUTION=Vanilla", "PROVIDER=Docker", "INIT=true",
		"ARGS="+args+" --kubeconfig "+filepath.Join(dir, "kubeconfig"),
		"SYSTEM_TEST_LOG_DIR="+dir, "FIXTURE_CALLS="+filepath.Join(dir, "calls"),
		"FIXTURE_TARGET="+filepath.Join(dir, "target.json"),
		"FIXTURE_CONTAINER="+filepath.Join(dir, "container.json"),
		"DOCKER_CONTEXT=fixture", "FIXTURE_DOCKER_ENDPOINT=unix:///var/run/docker.sock",
	)

	if mismatch == "remote Docker daemon" {
		command.Env = append(command.Env, "FIXTURE_DOCKER_ENDPOINT=tcp://unrelated.invalid:2375")
	}

	output, err := command.CombinedOutput()
	calls, readErr := fixtureRoot.ReadFile("calls")
	require.NoError(t, readErr)

	return string(output), string(calls), err
}

func TestHelmValuesPrecedenceRejectsUnrelatedClusterBeforeMutation(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ mismatch, want string }{
		{"missing name", "unambiguous explicit creation-time target is required"},
		{"other context", "kubeconfig does not identify the job-created local Kind container"},
		{"other Docker cluster", "kubeconfig does not identify the job-created local Kind container"},
		{"other API port", "kubeconfig does not identify the job-created local Kind container"},
		{"remote Docker daemon", "only the job-local Docker socket is permitted"},
	} {
		t.Run(testCase.mismatch, func(t *testing.T) {
			t.Parallel()

			output, calls, err := runHelmValuesTargetFixture(t, testCase.mismatch)
			require.Error(t, err, output)
			assert.Contains(t, output, testCase.want)
			assert.NotContains(t, calls, " create ")
			assert.NotContains(t, calls, " delete ")
			assert.NotContains(t, calls, " apply ")
		})
	}
}

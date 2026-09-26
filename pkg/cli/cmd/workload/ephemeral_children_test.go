package workload_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/svc/ephemeral"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

type childAdmission struct {
	admissionRecorder

	children []*unstructured.Unstructured
	err      error
	observed int
	roots    []*unstructured.Unstructured
}

func (c *childAdmission) ObserveChildren(
	_ context.Context,
	roots []*unstructured.Unstructured,
	_ time.Duration,
) ([]*unstructured.Unstructured, error) {
	c.observed++
	c.roots = roots

	return c.children, c.err
}

//nolint:paralleltest // replaces the isolated lifecycle/client factories
func TestValidateObservedChildFailsCELAndCleansUp(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	backend := installEphemeralProvisioner(t, fake, nil)
	child := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.test/v1",
		"kind":       "GeneratedWorkload",
		"metadata": map[string]any{
			"name":      "unsafe-child",
			"namespace": "default",
			"uid":       "child-uid",
		},
		"spec": map[string]any{"privileged": true},
	}}
	client := &childAdmission{admissionRecorder: admissionRecorder{
		apply: func(context.Context, *unstructured.Unstructured) error { return nil },
	}, children: []*unstructured.Unstructured{child}}
	restore := workload.ExportSetEphemeralAdmissionClient(
		func(string, string) (ephemeral.Client, error) { return client, nil },
	)
	t.Cleanup(restore)
	rules := writeRulesFile(t, `rules:
  - name: no-privileged-children
    expression: 'object.kind != "GeneratedWorkload" || !object.spec.privileged'
    message: "generated children must not be privileged"
    severity: error
`)
	root, schemaPath := childSource(t)
	_, err := runValidate(t, root, "--schema-location", schemaPath, "--rules", rules)
	require.NoError(t, err, "source passes before any child exists")
	assert.Zero(t, client.observed)

	cmd := workload.NewValidateCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(
		[]string{
			root,
			"--ephemeral",
			"--ephemeral-children",
			"--schema-location",
			schemaPath,
			"--rules",
			rules,
		},
	)
	err = cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "no-privileged-children")
	require.ErrorContains(t, err, "unsafe-child")
	require.ErrorContains(t, err, "operator-generated children")
	assert.Equal(t, 1, client.observed)
	require.Len(t, client.roots, 2)
	assert.ElementsMatch(
		t,
		[]string{"Generator", "ClusterPolicy"},
		[]string{client.roots[0].GetKind(), client.roots[1].GetKind()},
	)
	assert.Equal(t, fake.created, fake.deleted)
	assert.Equal(t, 1, backend.cleaned)
}

func TestChildObservationFlagsRequireEphemeral(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"--ephemeral-children"},
		{"--ephemeral", "--ephemeral-children", "--ephemeral-observation-wait", "0s"},
		{"--ephemeral", "--ephemeral-children", "--ephemeral-observation-wait", "6m"},
		{"--ephemeral-observation-wait", "1s"},
	} {
		t.Run(args[len(args)-1], func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewValidateCmd()
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(t.Context())
			require.Error(t, err)
			assert.NotContains(
				t,
				err.Error(),
				"unknown flag",
				"the feature must validate combinations before loading input or provisioning",
			)
		})
	}
}

func childSource(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"kustomization.yaml": "resources: [generator.yaml, namespace.yaml, policy.yaml]\n",
		"generator.yaml": `apiVersion: example.test/v1
kind: Generator
metadata:
  name: parent
  namespace: default
`,
		"namespace.yaml": `apiVersion: v1
kind: Namespace
metadata:
  name: default
  labels:
    environment: test
`,
		"policy.yaml": `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: safe-children
spec:
  validationFailureAction: Enforce
  background: false
  rules:
    - name: deny-privileged
      match:
        any:
          - resources:
              kinds: [GeneratedWorkload]
              namespaceSelector:
                matchLabels:
                  environment: test
      validate:
        message: generated children must not be privileged
        pattern:
          spec:
            privileged: false
`,
	}
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(data), 0o600))
	}

	schemaPath := filepath.Join(t.TempDir(), "schema.json")
	require.NoError(
		t,
		os.WriteFile(
			schemaPath,
			[]byte(
				`{"type":"object","properties":{"spec":{"type":"object","properties":{"privileged":{"type":"boolean"}}}}}`,
			),
			0o600,
		),
	)

	return root, schemaPath
}

func childAPI(t *testing.T, outcome string) http.HandlerFunc {
	t.Helper()

	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")

		if serveChildDiscovery(t, writer, request) {
			return
		}

		var body any

		switch request.URL.Path {
		case "/api/v1/namespaces/default",
			"/apis/kyverno.io/v1/clusterpolicies/safe-children",
			"/apis/example.test/v1/namespaces/default/generators/parent":
			body = submittedChildRoot(t, request)
		case "/apis/example.test/v1/generators":
			body = &unstructured.UnstructuredList{
				Object: map[string]any{"apiVersion": "example.test/v1", "kind": "GeneratorList"},
			}
		case "/apis/example.test/v1/generatedworkloads":
			if outcome == "list failure" {
				http.Error(writer, "forbidden", http.StatusForbidden)

				return
			}

			body = generatedChildList(outcome)
		default:
			t.Errorf("unexpected child API request: %s %s", request.Method, request.URL.Path)
			http.NotFound(writer, request)

			return
		}

		assert.NoError(t, json.NewEncoder(writer).Encode(body))
	}
}

//nolint:paralleltest // swaps only cluster creation/readiness; the admission and collection HTTP clients are real
func TestChildValidationThroughIsolatedAPI(t *testing.T) {
	for _, outcome := range []string{"safe", "schema", "kyverno", "skip kind", "empty", "list failure"} {
		t.Run(outcome, func(t *testing.T) {
			fake := &fakeEphemeralProvisioner{}
			backend := installEphemeralProvisioner(t, fake, nil)

			server := httptest.NewServer(childAPI(t, outcome))
			defer server.Close()

			installChildAPI(t, backend, server.URL)
			root, schemaPath := childSource(t)
			flags := []string{"--schema-location", schemaPath, "--kyverno-policies"}
			_, err := runValidate(t, append([]string{root}, flags...)...)
			require.NoError(t, err, "the exact source must pass the offline gate")

			cmd := workload.NewValidateCmd()

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)

			args := append(
				[]string{
					root,
					"--ephemeral",
					"--ephemeral-children",
					"--ephemeral-observation-wait",
					"1ms",
				},
				flags...)
			if outcome == "skip kind" {
				args = append(args, "--skip-kinds", "GeneratedWorkload")
			}

			cmd.SetArgs(args)
			err = cmd.ExecuteContext(t.Context())

			switch outcome {
			case "safe", "skip kind":
				require.NoError(t, err)
				assert.Contains(t, output.String(), "does not prove complete reconciliation")
			case "schema":
				require.ErrorContains(t, err, "privileged")
				require.ErrorContains(t, err, "observed-child")
			case "kyverno":
				require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
				require.ErrorContains(t, err, "observed-child")
			case "empty":
				require.ErrorIs(t, err, ephemeral.ErrObservation)
			case "list failure":
				require.ErrorContains(t, err, "forbidden")
			}

			assert.Equal(t, fake.created, fake.deleted)
			assert.Equal(t, 1, backend.cleaned)
			assert.NoDirExists(t, backend.workspace)
		})
	}
}

func submittedChildRoot(t *testing.T, request *http.Request) *unstructured.Unstructured {
	t.Helper()

	obj := &unstructured.Unstructured{}

	switch {
	case request.Method == http.MethodPatch:
		err := json.NewDecoder(request.Body).Decode(obj)
		if err != nil {
			t.Errorf("decode applied fixture: %v", err)
		}
	case request.URL.Path == "/apis/kyverno.io/v1/clusterpolicies/safe-children":
		obj.SetAPIVersion("kyverno.io/v1")
		obj.SetKind("ClusterPolicy")
		obj.SetName("safe-children")
	default:
		obj.SetAPIVersion("example.test/v1")
		obj.SetKind("Generator")
		obj.SetName("parent")
		obj.SetNamespace("default")
	}

	obj.SetUID(types.UID(obj.GetKind() + "-uid"))

	return obj
}

func generatedChildList(outcome string) *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{
		Object: map[string]any{"apiVersion": "example.test/v1", "kind": "GeneratedWorkloadList"},
	}

	if outcome != "empty" {
		var privileged any = outcome != "safe"
		if outcome == "schema" {
			privileged = "invalid-boolean"
		}

		list.Items = []unstructured.Unstructured{{Object: map[string]any{
			"apiVersion": "example.test/v1", "kind": "GeneratedWorkload",
			"metadata": map[string]any{
				"name":      "observed-child",
				"namespace": "default",
				"uid":       "child-uid",
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": "example.test/v1",
						"kind":       "Generator",
						"name":       "parent",
						"uid":        "Generator-uid",
					},
				},
			},
			"spec": map[string]any{"privileged": privileged},
		}}}
	}

	return list
}

func installChildAPI(t *testing.T, backend *fakeEphemeralBackend, serverURL string) {
	t.Helper()

	restore := workload.ExportSetEphemeralAdmissionClient(
		func(path, kubeContext string) (ephemeral.Client, error) {
			assert.Equal(t, filepath.Join(backend.workspace, "kubeconfig"), path)

			config := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: unrelated
clusters:
- name: isolated
  cluster:
    server: %s
contexts:
- name: %s
  context:
    cluster: isolated
    user: test
users:
- name: test
  user: {}
`, serverURL, kubeContext)

			err := os.WriteFile(path, []byte(config), 0o600)
			if err != nil {
				return nil, fmt.Errorf("write isolated test kubeconfig: %w", err)
			}

			return ephemeral.NewApplier(path, kubeContext)
		},
	)
	t.Cleanup(restore)
}

func serveChildDiscovery(t *testing.T, writer http.ResponseWriter, request *http.Request) bool {
	t.Helper()

	responses := map[string]string{
		"/api": `{"kind":"APIVersions","versions":["v1"]}`,
		"/apis": `{"kind":"APIGroupList","groups":[
   {"name":"example.test","preferredVersion":{"groupVersion":"example.test/v1","version":"v1"},
    "versions":[{"groupVersion":"example.test/v1","version":"v1"}]},
   {"name":"kyverno.io","preferredVersion":{"groupVersion":"kyverno.io/v1","version":"v1"},
    "versions":[{"groupVersion":"kyverno.io/v1","version":"v1"}]}
  ]}`,
		"/api/v1": `{"kind":"APIResourceList","groupVersion":"v1","resources":[
   {"name":"namespaces","kind":"Namespace","namespaced":false,"verbs":["get","patch"]}
  ]}`,
		"/apis/example.test/v1": `{"kind":"APIResourceList","groupVersion":"example.test/v1","resources":[
   {"name":"generators","kind":"Generator","namespaced":true,"verbs":["get","patch","list"]},
   {"name":"generatedworkloads","kind":"GeneratedWorkload","namespaced":true,"verbs":["get","list"]}
  ]}`,
		"/apis/kyverno.io/v1": `{"kind":"APIResourceList","groupVersion":"kyverno.io/v1","resources":[
   {"name":"clusterpolicies","kind":"ClusterPolicy","namespaced":false,"verbs":["get","patch"]}
  ]}`,
	}

	body, found := responses[request.URL.Path]
	if !found {
		return false
	}

	_, err := writer.Write([]byte(body))
	assert.NoError(t, err)

	return true
}

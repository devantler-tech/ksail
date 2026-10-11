package helm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	openapiv2 "github.com/google/gnostic-models/openapiv2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestTemplateChartPreservesLiveAdmissionAPIMigration(t *testing.T) {
	t.Parallel()

	for _, existing := range []bool{false, true} {
		t.Run(strconv.FormatBool(existing), func(t *testing.T) {
			t.Parallel()

			var writes atomic.Int32

			server := migrationServer(t, existing, &writes)
			client, chart := migrationClientAndChart(t, server.URL)
			manifest, err := client.TemplateChart(context.Background(), &helm.ChartSpec{
				ReleaseName: "migration-fixture", ChartName: chart, Namespace: "default",
				APIVersionMigrations: []helm.APIVersionMigration{{
					Kind: "MutatingAdmissionPolicy", From: "admissionregistration.k8s.io/v1beta1",
					To: "admissionregistration.k8s.io/v1",
				}},
			})
			require.NoError(t, err)
			require.Contains(t, manifest, "apiVersion: admissionregistration.k8s.io/v1\n")
			require.NotContains(t, manifest, "v1beta1")
			require.Zero(
				t,
				writes.Load(),
				"rendering must never mutate resources or release storage",
			)
		})
	}
}

func migrationServer(t *testing.T, existing bool, writes *atomic.Int32) *httptest.Server {
	t.Helper()

	responses := map[string]string{
		"/version": `{"major":"1","minor":"36","gitVersion":"v1.36.0"}`,
		"/api":     `{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`,
		"/apis": `{"kind":"APIGroupList","apiVersion":"v1","groups":[
{"name":"admissionregistration.k8s.io","versions":[
{"groupVersion":"admissionregistration.k8s.io/v1","version":"v1"}],
"preferredVersion":{"groupVersion":"admissionregistration.k8s.io/v1","version":"v1"}}]}`,
		"/api/v1": `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[]}`,
		"/apis/admissionregistration.k8s.io/v1": `{"kind":"APIResourceList","apiVersion":"v1",
"groupVersion":"admissionregistration.k8s.io/v1","resources":[
{"name":"mutatingadmissionpolicies","kind":"MutatingAdmissionPolicy","namespaced":false,
"verbs":["get","list","create","patch"]}]}`,
	}
	if existing {
		responses["/apis/admissionregistration.k8s.io/v1/mutatingadmissionpolicies/calico-migration-fixture"] = `{
"apiVersion":"admissionregistration.k8s.io/v1","kind":"MutatingAdmissionPolicy",
"metadata":{"name":"calico-migration-fixture","uid":"owned","resourceVersion":"7",
"labels":{"ksail.io/component":"calico-prerequisites"}},"spec":{"failurePolicy":"Fail"}}`
	}

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet {
				writes.Add(1)
			}

			writer.Header().Set("Content-Type", "application/json")

			if response, found := responses[request.URL.Path]; found {
				_, _ = writer.Write([]byte(response))

				return
			}

			if request.URL.Path == "/openapi/v2" {
				writeMigrationSchema(writer)

				return
			}

			writer.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(writer).
				Encode(map[string]string{"kind": "Status", "apiVersion": "v1", "reason": "NotFound"})
		}),
	)
	t.Cleanup(server.Close)

	return server
}

func writeMigrationSchema(writer http.ResponseWriter) {
	data, err := proto.Marshal(&openapiv2.Document{Swagger: "2.0"})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)

		return
	}

	writer.Header().
		Set("Content-Type", "application/com.github.proto-openapi.spec.v2.v1.0+protobuf")
	_, _ = writer.Write(data)
}

func migrationClientAndChart(t *testing.T, endpoint string) (*helm.Client, string) {
	t.Helper()
	root := t.TempDir()
	kubeconfig := filepath.Join(root, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfig, fmt.Appendf(nil, `apiVersion: v1
kind: Config
clusters:
- name: fixture
  cluster: {server: %s}
contexts:
- name: fixture
  context: {cluster: fixture, user: fixture}
current-context: fixture
users:
- name: fixture
  user: {}
`, endpoint), 0o600))

	chart := filepath.Join(root, "chart")
	require.NoError(t, os.MkdirAll(filepath.Join(chart, "templates"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(chart, "Chart.yaml"),
		[]byte("apiVersion: v2\nname: migration\nversion: 0.1.0\n"), 0o600))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(chart, "templates", "policy.yaml"),
			[]byte(`apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingAdmissionPolicy
metadata:
  name: calico-migration-fixture
spec:
  failurePolicy: Fail
`),
			0o600,
		),
	)

	client, err := helm.NewClient(kubeconfig, "fixture")
	require.NoError(t, err)

	return client, chart
}

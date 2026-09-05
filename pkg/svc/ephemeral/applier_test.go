package ephemeral_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/ephemeral"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func serveDiscovery(t *testing.T, writer http.ResponseWriter, request *http.Request) bool {
	t.Helper()

	writer.Header().Set("Content-Type", "application/json")

	var body any

	switch request.URL.Path {
	case "/api":
		body = map[string]any{"kind": "APIVersions", "versions": []string{"v1"}}
	case "/apis":
		body = map[string]any{"kind": "APIGroupList", "groups": []any{}}
	case "/api/v1":
		body = map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []any{
			map[string]any{
				"name":       "configmaps",
				"kind":       "ConfigMap",
				"namespaced": true,
				"verbs":      []string{"get", "patch", "create"},
			},
		}}
	default:
		return false
	}

	assert.NoError(t, json.NewEncoder(writer).Encode(body))

	return true
}

func kubeconfigFor(t *testing.T, url string) string {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, root, "config", `apiVersion: v1
kind: Config
current-context: wrong
clusters:
- name: isolated
  cluster:
    server: `+url+`
- name: wrong
  cluster:
    server: http://127.0.0.1:1
contexts:
- name: isolated
  context:
    cluster: isolated
    user: test
- name: wrong
  context:
    cluster: wrong
    user: test
users:
- name: test
  user: {}
`)

	return filepath.Join(root, "config")
}

func admissionServer(t *testing.T, rejected bool, requests chan<- string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if serveDiscovery(t, writer, request) {
				return
			}

			assert.Equal(t, http.MethodPatch, request.Method)
			assert.Equal(
				t,
				"/api/v1/namespaces/default/configmaps/settings",
				request.URL.Path,
			)
			assert.Equal(
				t,
				"application/apply-patch+yaml",
				request.Header.Get("Content-Type"),
			)
			assert.Equal(t, "Strict", request.URL.Query().Get("fieldValidation"))
			assert.Equal(t, "ksail-ephemeral", request.URL.Query().Get("fieldManager"))
			assert.Empty(t, request.URL.Query().Get("force"))

			requests <- request.URL.Path

			if rejected {
				writer.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = writer.Write(
					[]byte(
						`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Invalid",` +
							`"message":"admission rejected settings","code":422}`,
					),
				)

				return
			}

			_, _ = writer.Write(
				[]byte(
					`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"default"}}`,
				),
			)
		}),
	)
}

func TestApplierPinsContextAndRequestsStrictAdmission(t *testing.T) {
	t.Parallel()

	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "rejected"}[rejected], func(t *testing.T) {
			t.Parallel()

			requests := make(chan string, 1)

			server := admissionServer(t, rejected, requests)
			defer server.Close()

			client, err := ephemeral.NewApplier(kubeconfigFor(t, server.URL), "isolated")
			require.NoError(t, err)

			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]any{"name": "settings"},
			}}

			err = client.Apply(t.Context(), obj)
			if rejected {
				require.ErrorContains(t, err, "admission rejected settings")
			} else {
				require.NoError(t, err)
			}

			assert.Len(t, requests, 1)
			assert.Empty(t, obj.GetNamespace(), "applying must not mutate the plan")
		})
	}
}

func TestApplierRequiresExplicitConnection(t *testing.T) {
	t.Parallel()

	_, err := ephemeral.NewApplier("", "isolated")
	require.Error(t, err)
	_, err = ephemeral.NewApplier("config", "")
	require.Error(t, err)
}

func TestWaitForCRDHonorsEstablishmentAndCancellation(t *testing.T) {
	t.Parallel()

	for _, established := range []bool{true, false} {
		t.Run(
			map[bool]string{true: "established", false: "cancelled"}[established],
			func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				server := httptest.NewServer(
					http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						writer.Header().Set("Content-Type", "application/json")
						assert.Equal(
							t,
							"/apis/apiextensions.k8s.io/v1/customresourcedefinitions/widgets.example.com",
							request.URL.Path,
						)

						if !established {
							cancel()
						}

						status := "False"
						if established {
							status = "True"
						}

						body := map[string]any{
							"apiVersion": "apiextensions.k8s.io/v1",
							"kind":       "CustomResourceDefinition",
							"status": map[string]any{
								"conditions": []any{
									map[string]any{"type": "Established", "status": status},
								},
							},
						}
						assert.NoError(t, json.NewEncoder(writer).Encode(body))
					}),
				)
				defer server.Close()

				client, err := ephemeral.NewApplier(kubeconfigFor(t, server.URL), "isolated")
				require.NoError(t, err)

				bounded, stop := context.WithTimeout(ctx, time.Second)
				defer stop()

				err = client.WaitForCRD(bounded, "widgets.example.com")
				if established {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
			},
		)
	}
}

func TestApplierCancelsDiscoveryBeforePatch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodPatch {
				t.Error("PATCH after discovery cancellation")
			}

			cancel()
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusServiceUnavailable)
		}),
	)
	defer server.Close()

	client, err := ephemeral.NewApplier(kubeconfigFor(t, server.URL), "isolated")
	require.NoError(t, err)
	err = client.Apply(ctx, plannedResource("ConfigMap", "settings"))
	require.ErrorIs(t, err, context.Canceled)
}

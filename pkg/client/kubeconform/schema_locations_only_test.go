package kubeconform_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/stretchr/testify/require"
)

const selectionConfigMap = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: selection\ndata:\n  attempts: retry\n"

// TestOnlyLocationsNeverRestoreImplicitRegistries proves that a valid cached
// built-in cannot turn an empty or missing explicit selection into success.
//
//nolint:paralleltest // The existing cache fixture owns process-global cache environment variables.
func TestOnlyLocationsNeverRestoreImplicitRegistries(t *testing.T) {
	seedSchemaCache(t, map[string]string{
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
			"master-standalone-strict/configmap-v1.json": `{"type":"object"}`,
	})

	client := kubeconform.NewClient()
	for _, locations := range [][]string{nil, {""}, {" "}, {filepath.Join(t.TempDir(), "missing.json")}} {
		err := client.ValidateBytes(
			t.Context(),
			"selection.yaml",
			[]byte(selectionConfigMap),
			&kubeconform.ValidationOptions{
				Strict:              true,
				SchemaLocationsOnly: true,
				SchemaLocations:     locations,
			},
		)
		require.Error(
			t,
			err,
			"explicit selection must never silently restore defaults: %q",
			locations,
		)
	}

	require.NoError(
		t,
		client.ValidateBytes(
			t.Context(),
			"selection.yaml",
			[]byte(selectionConfigMap),
			&kubeconform.ValidationOptions{
				Strict: true,
			},
		),
		"ordinary built-in validation stays available",
	)
}

// TestOnlyLocationsKeepCallerOrder distinguishes schema choice from manifest
// validity: validation errors in the first existing schema never try a later one.
//
//nolint:paralleltest // The existing cache fixture owns process-global cache environment variables.
func TestOnlyLocationsKeepCallerOrder(t *testing.T) {
	seedSchemaCache(t, map[string]string{
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
			"master-standalone-strict/configmap-v1.json": `{"type":"object"}`,
	})
	first := filepath.Join(t.TempDir(), "first.json")
	second := filepath.Join(t.TempDir(), "second.json")
	require.NoError(
		t,
		os.WriteFile(first, []byte(`{"type":"object","required":["missing"]}`), 0o600),
	)
	require.NoError(t, os.WriteFile(second, []byte(`{"type":"object"}`), 0o600))

	client := kubeconform.NewClient()
	require.Error(t, client.ValidateBytes(
		t.Context(),
		"selection.yaml",
		[]byte(selectionConfigMap),
		&kubeconform.ValidationOptions{
			Strict:              true,
			SchemaLocationsOnly: true,
			SchemaLocations:     []string{first, second},
		},
	))
	require.NoError(
		t,
		client.ValidateBytes(
			t.Context(),
			"selection.yaml",
			[]byte(selectionConfigMap),
			&kubeconform.ValidationOptions{
				Strict:              true,
				SchemaLocationsOnly: true,
				SchemaLocations:     []string{second, first},
			},
		),
	)
	require.NoError(
		t,
		client.ValidateBytes(
			t.Context(),
			"selection.yaml",
			[]byte(selectionConfigMap),
			&kubeconform.ValidationOptions{
				Strict:              true,
				SchemaLocationsOnly: true,
				SchemaLocations:     []string{"default", first},
			},
		),
		"a caller can explicitly select the native built-in registry",
	)
}

// TestOnlyLocationsResolveCachedAndRelativeReferences exercises the native
// compiler and disk loader, including invalid data under each reference graph.
//
//nolint:paralleltest // The existing cache fixture owns process-global cache environment variables.
func TestOnlyLocationsResolveCachedAndRelativeReferences(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(
		http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			http.Error(response, "uncached reference unavailable", http.StatusServiceUnavailable)
		}),
	)
	t.Cleanup(server.Close)
	remote := server.URL + "/shared.json"
	seedSchemaCache(t, map[string]string{
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
			"master-standalone-strict/configmap-v1.json": `{"type":"object"}`,
		remote: `{"definitions":{"attempts":{"type":"string","enum":["retry"]}}}`,
	})
	root := t.TempDir()
	local := filepath.Join(root, "root.json")
	definition := []byte(`{"definitions":{"attempts":{"type":"string","enum":["retry"]}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "shared.json"), definition, 0o600))

	client := kubeconform.NewClient()

	for _, reference := range []string{
		"shared.json#/definitions/attempts",
		remote + "#/definitions/attempts",
	} {
		schema := `{"type":"object","properties":{"data":{"type":"object","properties":{"attempts":{"$ref":"` +
			reference + `"}}}}}`
		require.NoError(t, os.WriteFile(local, []byte(schema), 0o600))
		opts := &kubeconform.ValidationOptions{
			Strict:              true,
			SchemaLocationsOnly: true,
			SchemaLocations:     []string{local},
		}
		require.NoError(
			t,
			client.ValidateBytes(t.Context(), "selection.yaml", []byte(selectionConfigMap), opts),
			reference,
		)

		invalid := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: selection\ndata:\n  attempts: invalid\n"
		require.Error(
			t,
			client.ValidateBytes(t.Context(), "selection.yaml", []byte(invalid), opts),
			reference,
		)
	}

	require.Zero(t, requests.Load(), "complete cached reference graphs make no requests")
}

// TestOnlyLocationsMissingReferenceFails preserves native reference-loader failures.
//
//nolint:paralleltest // The existing cache fixture owns process-global cache environment variables.
func TestOnlyLocationsMissingReferenceFails(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(
		http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			http.Error(response, "uncached reference unavailable", http.StatusServiceUnavailable)
		}),
	)
	t.Cleanup(server.Close)
	seedSchemaCache(t, map[string]string{
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
			"master-standalone-strict/configmap-v1.json": `{"type":"object"}`,
	})
	local := filepath.Join(t.TempDir(), "root.json")

	missing := `{"type":"object","properties":{"data":{"$ref":"` + server.URL + `/absent.json"}}}`
	require.NoError(t, os.WriteFile(local, []byte(missing), 0o600))
	require.Error(t, kubeconform.NewClient().ValidateBytes(
		t.Context(),
		"selection.yaml",
		[]byte(selectionConfigMap),
		&kubeconform.ValidationOptions{
			SchemaLocationsOnly: true,
			SchemaLocations:     []string{local},
		},
	))
	require.Positive(
		t,
		requests.Load(),
		"selection is not a transport prohibition; missing references still fail",
	)
}

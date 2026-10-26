package workload_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/stretchr/testify/require"
	"github.com/yannh/kubeconform/pkg/cache"
)

const (
	typedLocationSchema = `{
  "type": "object",
  "properties": {
    "spec": {
      "type": "object",
      "properties": {
        "size": {
          "type": "integer"
        },
        "enabled": {
          "type": "boolean"
        }
      },
      "required": [
        "size",
        "enabled"
      ],
      "additionalProperties": false
    }
  },
  "required": [
    "spec"
  ]
}`
	typedLocationManifest = `apiVersion: supplied.ksail.io/v1
kind: SuppliedTypes
metadata:
  name: widget
spec:
  size: ${SIZE}
  enabled: ${ENABLED}
`
)

// TestValidateSuppliedSchemaTypes exercises the real command's loose-file,
// Kustomize and Helm-rendering paths. A supplied schema must determine Flux
// placeholder types, and an invalid supplied default must still fail validation.
//
//nolint:paralleltest // Each fixture owns the process-wide schema-cache environment.
func TestValidateSuppliedSchemaTypes(t *testing.T) {
	for _, source := range []string{"local", "remote", "remote-directory"} {
		for _, layout := range []string{"loose", "kustomize", "rendered"} {
			t.Run(source+"/"+layout, func(t *testing.T) {
				seedSuppliedTypesCache(t)
				location := suppliedTypesLocation(t, source)
				dir := t.TempDir()
				manifest := filepath.Join(dir, "widget.yaml")
				require.NoError(t, os.WriteFile(manifest, []byte(typedLocationManifest), 0o600))

				if layout != "loose" {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"),
						[]byte("resources:\n  - widget.yaml\n"), 0o600))
				}

				args := []string{
					dir,
					"--strict",
					"--ignore-missing-schemas=false",
					"--schema-location",
					location,
				}
				if layout != "rendered" {
					args = append(args, "--skip-helm-render")
				}

				output, err := runValidate(t, args...)
				require.NoError(
					t,
					err,
					"typed placeholders must use the supplied schema: %s",
					output,
				)

				invalid := strings.Replace(typedLocationManifest, "${SIZE}", "${SIZE:-3oops}", 1)
				require.NoError(t, os.WriteFile(manifest, []byte(invalid), 0o600))
				_, err = runValidate(t, args...)
				require.Error(t, err, "a non-integer Flux default must remain invalid")
			})
		}
	}
}

// seedSuppliedTypesCache isolates cache paths and supplies deterministic built-in misses.
func seedSuppliedTypesCache(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, home)
	}

	cacheDir := fluxsubst.SchemaCacheDir()
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))

	store := cache.NewOnDiskCache(cacheDir)
	for _, url := range []string{
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
			"master-standalone/suppliedtypes-supplied-v1.json",
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
			"master-standalone-strict/suppliedtypes-supplied-v1.json",
		"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/supplied.ksail.io/suppliedtypes_v1.json",
	} {
		require.NoError(t, store.Set(url, []byte("null")))
	}
}

// suppliedTypesLocation supplies a native local registry or an already warmed offline remote registry.
func suppliedTypesLocation(t *testing.T, source string) string {
	t.Helper()

	if source == "local" {
		dir := t.TempDir()
		path := filepath.Join(dir, "master-standalone-strict")
		require.NoError(t, os.MkdirAll(path, 0o700))
		require.NoError(
			t,
			os.WriteFile(
				filepath.Join(path, "suppliedtypes-supplied-v1.json"),
				[]byte(typedLocationSchema),
				0o600,
			),
		)

		return dir
	}

	server := httptest.NewServer(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			expectedPath := "/supplied.ksail.io/suppliedtypes_v1.json"
			if source == "remote-directory" {
				expectedPath = "/master-standalone-strict/suppliedtypes-supplied-v1.json"
			}

			if request.URL.Path != expectedPath {
				http.NotFound(response, request)

				return
			}

			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(typedLocationSchema))
		}),
	)
	t.Cleanup(server.Close)

	location := server.URL + "/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json"
	if source == "remote-directory" {
		location = server.URL
	}

	warm := []byte(
		"apiVersion: supplied.ksail.io/v1\nkind: SuppliedTypes\nspec:\n  size: 3\n  enabled: true\n",
	)
	require.NoError(
		t,
		kubeconform.NewClient().ValidateBytes(context.Background(), "warm.yaml", warm,
			&kubeconform.ValidationOptions{Strict: true, SchemaLocations: []string{location}}),
	)
	// The command must use cached remote schema bytes for substitution offline.
	server.Close()

	return location
}

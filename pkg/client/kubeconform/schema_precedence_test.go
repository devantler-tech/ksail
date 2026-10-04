package kubeconform_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/yannh/kubeconform/pkg/cache"
)

const currentWidgetSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "apiVersion": {"type": "string"},
    "kind": {"type": "string"},
    "metadata": {"type": "object"},
    "spec": {
      "type": "object",
      "properties": {
        "size": {"type": "integer"},
        "listenerConfig": {"type": "object"}
      },
      "required": ["size"],
      "additionalProperties": false
    }
  },
  "required": ["spec"]
}`

const widgetWithListener = `apiVersion: example.com/v1
kind: WidgetThing
metadata:
  name: widget
spec:
  size: 3
  listenerConfig: {}
`

// TestValidateBytesSchemaPrecedence exercises the production client against
// isolated cached registries. A stale catalogue must not mask a supplied CRD
// schema, while invalid resources and missing schemas must still fail.
func TestValidateBytesSchemaPrecedence(t *testing.T) {
	cases := []struct {
		name     string
		catalog  string
		supplied string
		manifest string
		wantErr  bool
	}{
		{
			name:    "current supplied CRD wins over stale catalogue",
			catalog: widgetCRDSchema, supplied: currentWidgetSchema,
			manifest: widgetWithListener,
		},
		{
			name:    "supplied schema still rejects invalid fields",
			catalog: widgetCRDSchema, supplied: currentWidgetSchema,
			manifest: invalidWidgetCR, wantErr: true,
		},
		{
			name:    "catalogue remains available without supplied schemas",
			catalog: widgetCRDSchema, manifest: validWidgetCR,
		},
		{
			name:    "stale catalogue alone rejects the newer field",
			catalog: widgetCRDSchema, manifest: widgetWithListener, wantErr: true,
		},
		{
			name:    "missing supplied schema falls back to catalogue",
			catalog: widgetCRDSchema, supplied: "missing",
			manifest: validWidgetCR,
		},
		{
			name:    "malformed supplied schema cannot pass without another schema",
			catalog: "null", supplied: "{ malformed",
			manifest: validWidgetCR, wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedSchemaCache(t, map[string]string{
				// A null cached response exercises kubeconform's absent-schema
				// fallback without reaching either public registry over the network.
				"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
					"master-standalone-strict/widgetthing-example-v1.json": "null",
				"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/" +
					"example.com/widgetthing_v1.json": tc.catalog,
			})
			opts := &kubeconform.ValidationOptions{Strict: true}
			if tc.supplied != "" {
				location := filepath.Join(t.TempDir(), "{{.ResourceKind}}_{{.ResourceAPIVersion}}.json")
				opts.SchemaLocations = []string{location}
				if tc.supplied != "missing" {
					writeSchemaFixture(t, filepath.Join(filepath.Dir(location), "widgetthing_v1.json"), tc.supplied)
				}
			}

			err := kubeconform.NewClient().ValidateBytes(context.Background(), "widget.yaml", []byte(tc.manifest), opts)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validation error = %v; want error = %v", err, tc.wantErr)
			}
		})
	}
}

// TestValidateBytesBuiltinSchemaKeepsPriority prevents supplied CRD schemas
// from overriding validation of built-in Kubernetes resources.
func TestValidateBytesBuiltinSchemaKeepsPriority(t *testing.T) {
	seedSchemaCache(t, map[string]string{
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/" +
			"master-standalone-strict/namespace-v1.json": `{"type":"object","properties":{
      "apiVersion":{"type":"string"},"kind":{"type":"string"},"metadata":{"type":"object"}
    },"additionalProperties":false}`,
		"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/v1/namespace_v1.json": `{}`,
	})
	location := filepath.Join(t.TempDir(), "namespace_v1.json")
	writeSchemaFixture(t, location, `{}`)
	opts := &kubeconform.ValidationOptions{Strict: true, SchemaLocations: []string{location}}
	client := kubeconform.NewClient()
	if err := client.ValidateBytes(context.Background(), "namespace.yaml", []byte(validNamespaceYAML), opts); err != nil {
		t.Fatalf("valid built-in resource failed: %v", err)
	}
	if err := client.ValidateBytes(context.Background(), "namespace.yaml", []byte(validNamespaceYAML+"bogus: true\n"), opts); err == nil {
		t.Fatal("permissive supplied schema bypassed the built-in schema")
	}
}

// seedSchemaCache isolates every platform's cache environment before seeding
// deterministic registry responses consumed by the real validation client.
func seedSchemaCache(t *testing.T, schemas map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, dir)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(base, "ksail", "kubeconform")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	registryCache := cache.NewOnDiskCache(cacheDir)
	for url, schema := range schemas {
		if err := registryCache.Set(url, []byte(schema)); err != nil {
			t.Fatalf("seed schema cache: %v", err)
		}
	}
}

// writeSchemaFixture creates a private schema file for validation fixtures.
func writeSchemaFixture(t *testing.T, path, schema string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
}

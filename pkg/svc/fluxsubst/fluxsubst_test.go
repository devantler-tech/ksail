package fluxsubst_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yannh/kubeconform/pkg/cache"
)

const integerType = "integer"

func TestExpandFluxSubstitutions_NoVariablesPassThrough(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\n")

	got := fluxsubst.ExpandFluxSubstitutions(input)
	if string(got) != string(input) {
		t.Fatalf("expected pass-through, got %q", got)
	}
}

func TestExpandFluxSubstitutions_DefaultUsed(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\ndata:\n  key: ${MY_VAR:-fallback}\n")

	got := string(fluxsubst.ExpandFluxSubstitutions(input))
	if !strings.Contains(got, "fallback") {
		t.Fatalf("expected default value substituted, got %q", got)
	}

	if strings.Contains(got, "${") {
		t.Fatalf("expected variable reference removed, got %q", got)
	}
}

func TestExpandFluxSubstitutions_BareVarBecomesPlaceholder(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\ndata:\n  key: ${MY_VAR}\n")

	got := string(fluxsubst.ExpandFluxSubstitutions(input))
	if !strings.Contains(got, "placeholder") {
		t.Fatalf("expected placeholder substituted for bare var, got %q", got)
	}
}

func TestTypedPlaceholderValue(t *testing.T) {
	t.Parallel()

	tests := map[string]any{
		integerType: 0,
		"number":    0.0,
		"boolean":   true,
		"string":    "placeholder",
		"":          "placeholder",
	}

	for schemaType, want := range tests {
		if got := fluxsubst.TypedPlaceholderValue(schemaType); got != want {
			t.Errorf("TypedPlaceholderValue(%q) = %v, want %v", schemaType, got, want)
		}
	}
}

func TestSplitAPIVersion(t *testing.T) {
	t.Parallel()

	group, version := fluxsubst.SplitAPIVersion("apps/v1")
	if group != "apps" || version != "v1" {
		t.Fatalf("SplitAPIVersion(apps/v1) = (%q,%q), want (apps,v1)", group, version)
	}

	group, version = fluxsubst.SplitAPIVersion("v1")
	if group != "" || version != "v1" {
		t.Fatalf("SplitAPIVersion(v1) = (%q,%q), want (,v1)", group, version)
	}
}

func TestSchemaURLs_MatchKubeconformLocations(t *testing.T) {
	t.Parallel()

	const (
		builtin = "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/"
		catalog = "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/"
	)

	tests := []struct {
		apiVersion string
		kind       string
		want       []string
	}{
		{
			apiVersion: "v1",
			kind:       "ConfigMap",
			want: []string{
				builtin + "master-standalone/configmap-v1.json",
				builtin + "master-standalone-strict/configmap-v1.json",
			},
		},
		{
			apiVersion: "apps/v1",
			kind:       "Deployment",
			want: []string{
				builtin + "master-standalone/deployment-apps-v1.json",
				builtin + "master-standalone-strict/deployment-apps-v1.json",
				catalog + "apps/deployment_v1.json",
			},
		},
		{
			// kubeconform keeps only the first label of a dotted API group.
			apiVersion: "networking.k8s.io/v1",
			kind:       "Ingress",
			want: []string{
				builtin + "master-standalone/ingress-networking-v1.json",
				builtin + "master-standalone-strict/ingress-networking-v1.json",
				catalog + "networking.k8s.io/ingress_v1.json",
			},
		},
		{
			apiVersion: "helm.toolkit.fluxcd.io/v2",
			kind:       "HelmRelease",
			want: []string{
				builtin + "master-standalone/helmrelease-helm-v2.json",
				builtin + "master-standalone-strict/helmrelease-helm-v2.json",
				catalog + "helm.toolkit.fluxcd.io/helmrelease_v2.json",
			},
		},
	}

	for _, test := range tests {
		got := fluxsubst.SchemaURLs(test.apiVersion, test.kind)
		if !slices.Equal(got, test.want) {
			t.Errorf("SchemaURLs(%q, %q) = %q, want %q", test.apiVersion, test.kind, got, test.want)
		}
	}
}

// TestExpandFluxSubstitutions_UsesKubeconformCache warms the schema cache the way
// kubeconform does, through its own cache package, and expects the typed placeholder.
func TestExpandFluxSubstitutions_UsesKubeconformCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	cacheDir := fluxsubst.SchemaCacheDir()
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))

	// A kind no other test uses, so the process-wide schema registry has no entry for it.
	schemaURL := "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/" +
		"example.ksail.io/substcachewidget_v1.json"
	schema := []byte(`{"properties":{"spec":{"properties":{"size":{"type":"integer"}}}}}`)
	require.NoError(t, cache.NewOnDiskCache(cacheDir).Set(schemaURL, schema))

	input := []byte("apiVersion: example.ksail.io/v1\nkind: SubstCacheWidget\n" +
		"spec:\n  size: ${SIZE}\n")

	got := string(fluxsubst.ExpandFluxSubstitutions(input))
	assert.Contains(t, got, "size: 0")
	assert.NotContains(t, got, "placeholder")
}

func TestGetSchemaTypeAtPath(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"properties": map[string]any{
			"spec": map[string]any{
				"properties": map[string]any{
					"replicas": map[string]any{"type": integerType},
				},
			},
		},
	}

	if got := fluxsubst.GetSchemaTypeAtPath(schema, "/spec/replicas"); got != integerType {
		t.Fatalf("GetSchemaTypeAtPath = %q, want integer", got)
	}

	if got := fluxsubst.GetSchemaTypeAtPath(nil, "/spec"); got != "" {
		t.Fatalf("GetSchemaTypeAtPath(nil) = %q, want empty", got)
	}
}

func TestIsNumericIndex(t *testing.T) {
	t.Parallel()

	if !fluxsubst.IsNumericIndex("42") {
		t.Fatal("expected 42 to be a numeric index")
	}

	if fluxsubst.IsNumericIndex("abc") || fluxsubst.IsNumericIndex("") {
		t.Fatal("expected non-digit and empty strings to be rejected")
	}
}

package fluxsubst_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
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

func TestSchemaURLs_CoreVsGroup(t *testing.T) {
	t.Parallel()

	core := fluxsubst.SchemaURLs("v1", "ConfigMap")
	if len(core) != 1 {
		t.Fatalf("core SchemaURLs len = %d, want 1", len(core))
	}

	grouped := fluxsubst.SchemaURLs("apps/v1", "Deployment")
	if len(grouped) != 2 {
		t.Fatalf("grouped SchemaURLs len = %d, want 2", len(grouped))
	}
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

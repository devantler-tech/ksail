package fluxsubst_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
)

const (
	stringType  = "string"
	booleanType = "boolean"
)

func TestParseInteger(t *testing.T) {
	t.Parallel()

	tests := map[string]any{
		"42":  int64(42),
		"0":   int64(0),
		"-17": int64(-17),
		"abc": "abc", // not parseable → defaultVal returned verbatim
		"":    "",    // empty → defaultVal returned verbatim
	}

	for in, want := range tests {
		if got := fluxsubst.ParseInteger(in, in); got != want {
			t.Errorf("ParseInteger(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestParseNumber(t *testing.T) {
	t.Parallel()

	tests := map[string]any{
		"2.5":       2.5,
		"10":        10.0,
		"notafloat": "notafloat", // not parseable → defaultVal returned verbatim
		"":          "",          // empty → defaultVal returned verbatim
	}

	for in, want := range tests {
		if got := fluxsubst.ParseNumber(in, in); got != want {
			t.Errorf("ParseNumber(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestParseBoolean(t *testing.T) {
	t.Parallel()

	tests := map[string]any{
		"true":  true,
		"false": false,
		"yes":   "yes", // only "true"/"false" parse → defaultVal returned verbatim
		"":      "",
	}

	for in, want := range tests {
		if got := fluxsubst.ParseBoolean(in, in); got != want {
			t.Errorf("ParseBoolean(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestInferYAMLType(t *testing.T) {
	t.Parallel()

	// YAML-native inference: "true" → bool, "hello" → string, "3" → numeric.
	if got := fluxsubst.InferYAMLType("true", "d"); got != true {
		t.Errorf("InferYAMLType(true) = %#v, want true", got)
	}

	if got := fluxsubst.InferYAMLType("hello", "d"); got != "hello" {
		t.Errorf("InferYAMLType(hello) = %#v, want \"hello\"", got)
	}

	// Numeric type may be int or float depending on the YAML codec; assert on the
	// rendered value rather than the concrete Go type to stay codec-robust.
	if got := fmt.Sprintf("%v", fluxsubst.InferYAMLType("3", "d")); got != "3" {
		t.Errorf("InferYAMLType(3) rendered = %q, want \"3\"", got)
	}

	// Empty input cannot be inferred → defaultVal is returned verbatim.
	if got := fluxsubst.InferYAMLType("", "fallback"); got != "fallback" {
		t.Errorf("InferYAMLType(\"\") = %#v, want \"fallback\"", got)
	}
}

func TestParseJSONSchema(t *testing.T) {
	t.Parallel()

	schema := fluxsubst.ParseJSONSchema([]byte(`{"type":"string"}`))
	if schema == nil || schema["type"] != stringType {
		t.Fatalf("ParseJSONSchema(valid) = %#v, want map with type=string", schema)
	}

	if got := fluxsubst.ParseJSONSchema([]byte(`{not valid json`)); got != nil {
		t.Errorf("ParseJSONSchema(malformed) = %#v, want nil", got)
	}

	// A JSON array is valid JSON but not a schema object → nil.
	if got := fluxsubst.ParseJSONSchema([]byte(`[1,2,3]`)); got != nil {
		t.Errorf("ParseJSONSchema(array) = %#v, want nil", got)
	}
}

func TestSchemaCacheDir(t *testing.T) {
	t.Parallel()

	got := fluxsubst.SchemaCacheDir()
	if got == "" {
		t.Fatal("SchemaCacheDir() returned empty string")
	}

	suffix := filepath.Join("ksail", "kubeconform")
	if !strings.HasSuffix(got, suffix) {
		t.Errorf("SchemaCacheDir() = %q, want suffix %q", got, suffix)
	}
}

func TestSchemaCacheFileName(t *testing.T) {
	t.Parallel()

	// "://", "/", and "." are each replaced with "_", then ".json" is appended.
	if got := fluxsubst.SchemaCacheFileName("a://b/c.d"); got != "a_b_c_d.json" {
		t.Errorf("SchemaCacheFileName(a://b/c.d) = %q, want a_b_c_d.json", got)
	}

	// Over-long names are truncated to a bounded length while keeping the suffix.
	long := "http://example.com/" + strings.Repeat("seg/", 200) + "schema.json"

	got := fluxsubst.SchemaCacheFileName(long)
	if len(got) != 200 {
		t.Errorf("SchemaCacheFileName(long) len = %d, want 200", len(got))
	}

	if !strings.HasSuffix(got, ".json") {
		t.Errorf("SchemaCacheFileName(long) = %q, want .json suffix", got)
	}
}

func TestSchemaNodeType(t *testing.T) {
	t.Parallel()

	if got := fluxsubst.SchemaNodeType(map[string]any{"type": "integer"}); got != "integer" {
		t.Errorf("SchemaNodeType(string type) = %q, want integer", got)
	}

	// Union types: the first non-"null" entry wins (nullable schemas).
	union := map[string]any{"type": []any{"null", booleanType}}
	if got := fluxsubst.SchemaNodeType(union); got != booleanType {
		t.Errorf("SchemaNodeType(union) = %q, want boolean", got)
	}

	if got := fluxsubst.SchemaNodeType(map[string]any{}); got != "" {
		t.Errorf("SchemaNodeType(no type) = %q, want empty", got)
	}

	// A non-string, non-array type value is unresolvable → empty.
	if got := fluxsubst.SchemaNodeType(map[string]any{"type": 123}); got != "" {
		t.Errorf("SchemaNodeType(numeric type) = %q, want empty", got)
	}
}

func TestResolveFromProperties(t *testing.T) {
	t.Parallel()

	child := map[string]any{"type": "integer"}
	schema := map[string]any{"properties": map[string]any{"replicas": child}}

	if got := fluxsubst.ResolveFromProperties(schema, "replicas"); got["type"] != "integer" {
		t.Errorf("ResolveFromProperties(replicas) = %#v, want child node", got)
	}

	if got := fluxsubst.ResolveFromProperties(schema, "missing"); got != nil {
		t.Errorf("ResolveFromProperties(missing) = %#v, want nil", got)
	}

	if got := fluxsubst.ResolveFromProperties(map[string]any{}, "x"); got != nil {
		t.Errorf("ResolveFromProperties(no properties) = %#v, want nil", got)
	}
}

func TestResolveFromItems(t *testing.T) {
	t.Parallel()

	items := map[string]any{"type": "string"}
	schema := map[string]any{"items": items}

	// A numeric-index key navigates into the array item schema.
	if got := fluxsubst.ResolveFromItems(schema, "0"); got["type"] != stringType {
		t.Errorf("ResolveFromItems(0) = %#v, want items node", got)
	}

	// A non-numeric key is not an array index → nil.
	if got := fluxsubst.ResolveFromItems(schema, "name"); got != nil {
		t.Errorf("ResolveFromItems(name) = %#v, want nil", got)
	}

	if got := fluxsubst.ResolveFromItems(map[string]any{}, "0"); got != nil {
		t.Errorf("ResolveFromItems(no items) = %#v, want nil", got)
	}
}

func TestResolveFromCombiners(t *testing.T) {
	t.Parallel()

	target := map[string]any{"type": booleanType}

	for _, combiner := range []string{"allOf", "anyOf", "oneOf"} {
		schema := map[string]any{
			combiner: []any{
				map[string]any{"properties": map[string]any{"enabled": target}},
			},
		}

		if got := fluxsubst.ResolveFromCombiners(schema, "enabled"); got["type"] != booleanType {
			t.Errorf("ResolveFromCombiners(%s/enabled) = %#v, want target node", combiner, got)
		}
	}

	if got := fluxsubst.ResolveFromCombiners(map[string]any{}, "x"); got != nil {
		t.Errorf("ResolveFromCombiners(no combiners) = %#v, want nil", got)
	}
}

// TestExpandFluxSubstitutions_FallbackPath exercises the regex fallback used when
// a document is a bare scalar (not a YAML map/list root).
func TestExpandFluxSubstitutions_FallbackPath(t *testing.T) {
	t.Parallel()

	// Scalar-root doc with a defaulted var → fallback resolves to the default.
	got := string(fluxsubst.ExpandFluxSubstitutions([]byte("${MY_VAR:-resolved}")))
	if !strings.Contains(got, "resolved") || strings.Contains(got, "${") {
		t.Errorf("fallback default expansion = %q, want \"resolved\" and no \"${\"", got)
	}

	// Scalar-root bare var → fallback resolves to the string placeholder.
	got = string(fluxsubst.ExpandFluxSubstitutions([]byte("${MY_VAR}")))
	if !strings.Contains(got, "placeholder") {
		t.Errorf("fallback bare expansion = %q, want \"placeholder\"", got)
	}
}

// TestExpandFluxSubstitutions_MixedText exercises expandMixedText for values that
// embed a variable reference within surrounding text (always string context).
func TestExpandFluxSubstitutions_MixedText(t *testing.T) {
	t.Parallel()

	bare := string(fluxsubst.ExpandFluxSubstitutions(
		[]byte("apiVersion: v1\nkind: ConfigMap\ndata:\n  key: pre-${VAR}-post\n"),
	))
	if !strings.Contains(bare, "pre-placeholder-post") {
		t.Errorf("mixed bare expansion = %q, want \"pre-placeholder-post\"", bare)
	}

	defaulted := string(fluxsubst.ExpandFluxSubstitutions(
		[]byte("apiVersion: v1\nkind: ConfigMap\ndata:\n  key: pre-${VAR:-mid}-post\n"),
	))
	if !strings.Contains(defaulted, "pre-mid-post") {
		t.Errorf("mixed default expansion = %q, want \"pre-mid-post\"", defaulted)
	}
}

// TestExpandFluxSubstitutions_ListRoot exercises expandListDocument for a YAML
// document whose root is a list (e.g. a JSON6902 patch list), which is walked
// with a nil schema.
func TestExpandFluxSubstitutions_ListRoot(t *testing.T) {
	t.Parallel()

	input := []byte("- op: replace\n  path: /spec/x\n  value: ${VAR:-listval}\n")

	got := string(fluxsubst.ExpandFluxSubstitutions(input))
	if !strings.Contains(got, "listval") || strings.Contains(got, "${") {
		t.Errorf("list-root expansion = %q, want \"listval\" and no \"${\"", got)
	}
}

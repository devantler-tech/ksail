package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGeneratedSchema_AlphaPlaceholderDescriptions(t *testing.T) {
	// Black-box test: execute the generator and validate schema output.
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "ksail-config.schema.json")

	cmd := exec.Command("go", "run", ".", outPath)
	cmd.Dir = "." // package directory: .github/scripts/generate-schema

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generator failed: %v\noutput:\n%s", err, string(out))
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read generated schema: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatalf("unmarshal generated schema: %v", err)
	}

	alphaKeys := []string{"kind", "k3d", "cilium", "calico", "argocd", "helm", "kustomize"}
	for _, key := range alphaKeys {
		desc := mustGetOptionsPropertyDescription(t, schema, key)
		if desc == "" {
			t.Fatalf("expected options.%s to have a description", key)
		}
		if desc != "Alpha placeholder (currently unsupported)." && !contains(desc, "Alpha placeholder") {
			t.Fatalf("expected options.%s description to contain alpha placeholder note, got %q", key, desc)
		}
	}
}

func mustGetOptionsPropertyDescription(t *testing.T, schema map[string]any, key string) string {
	t.Helper()

	properties := mustMap(t, schema["properties"], "properties")
	spec := mustMap(t, properties["spec"], "properties.spec")
	specProps := mustMap(t, spec["properties"], "properties.spec.properties")
	options := mustMap(t, specProps["options"], "properties.spec.properties.options")
	optionsProps := mustMap(t, options["properties"], "properties.spec.properties.options.properties")
	prop := mustMap(t, optionsProps[key], "properties.spec.properties.options.properties."+key)

	desc, _ := prop["description"].(string)
	return desc
}

func mustMap(t *testing.T, v any, path string) map[string]any {
	t.Helper()

	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected %s to be an object, got %T", path, v)
	}

	return m
}

func contains(haystack, needle string) bool {
	// Avoid bringing in extra deps for a single substring check.
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}

	return false
}

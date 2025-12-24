package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedSchema_AlphaPlaceholderDescriptions(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "ksail-config.schema.json")

	cmd := exec.Command("go", "run", ".", outPath)
	cmd.Dir = "."

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

	// These option keys should be marked as alpha placeholders (empty structs)
	alphaKeys := []string{"kind", "k3d", "cilium", "calico", "argocd", "helm", "kustomize"}
	for _, key := range alphaKeys {
		desc := getOptionsPropertyDescription(t, schema, key)
		if !strings.Contains(desc, "Alpha placeholder") {
			t.Errorf("expected options.%s description to contain 'Alpha placeholder', got %q", key, desc)
		}
	}

	// These option keys should NOT be marked as alpha (they have properties)
	nonAlphaKeys := []string{"flux", "localRegistry"}
	for _, key := range nonAlphaKeys {
		desc := getOptionsPropertyDescription(t, schema, key)
		if strings.Contains(desc, "Alpha placeholder") {
			t.Errorf("expected options.%s to NOT have alpha placeholder, got %q", key, desc)
		}
	}
}

func getOptionsPropertyDescription(t *testing.T, schema map[string]any, key string) string {
	t.Helper()

	// Path: properties.spec.properties.cluster.properties.options.properties.<key>
	props := mustMap(t, schema["properties"], "properties")
	spec := mustMap(t, props["spec"], "spec")
	specProps := mustMap(t, spec["properties"], "spec.properties")
	cluster := mustMap(t, specProps["cluster"], "cluster")
	clusterProps := mustMap(t, cluster["properties"], "cluster.properties")
	options := mustMap(t, clusterProps["options"], "options")
	optionsProps := mustMap(t, options["properties"], "options.properties")
	prop := mustMap(t, optionsProps[key], "options."+key)

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

// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the MIT License.

package schemas_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// generateSchema runs the schema generator and returns the parsed JSON.
func generateSchema(t *testing.T) map[string]any {
	t.Helper()

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "ksail-config.schema.json")

	// Run the generator from the schemas/ directory.
	cmd := exec.CommandContext( //nolint:gosec // test-controlled arguments
		context.Background(),
		"go",
		"run",
		"gen_schema.go",
		outPath,
	)
	cmd.Dir = filepath.Join("..", "schemas")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generator failed: %v\noutput:\n%s", err, string(out))
	}

	schemaData, err := os.ReadFile(outPath) //nolint:gosec // path from t.TempDir, not user input
	if err != nil {
		t.Fatalf("read generated schema: %v", err)
	}

	var schema map[string]any

	err = json.Unmarshal(schemaData, &schema)
	if err != nil {
		t.Fatalf("unmarshal generated schema: %v", err)
	}

	return schema
}

func TestGeneratedSchema(t *testing.T) {
	t.Parallel()

	schema := generateSchema(t)

	t.Run("root metadata", func(t *testing.T) {
		t.Parallel()

		if got := schema["title"]; got != "KSail Cluster Configuration" {
			t.Errorf("title = %q, want %q", got, "KSail Cluster Configuration")
		}

		if got := schema["additionalProperties"]; got != false {
			t.Errorf("additionalProperties = %v, want false", got)
		}

		req, ok := schema["required"].([]any)
		if !ok || len(req) != 1 || req[0] != "spec" {
			t.Errorf("required = %v, want [spec]", schema["required"])
		}
	})

	t.Run("kind enum", func(t *testing.T) {
		t.Parallel()

		kindProp := mustProp(t, schema, "kind")
		assertEnum(t, kindProp, []string{"Cluster"})
	})
	t.Run("apiVersion enum", func(t *testing.T) {
		t.Parallel()

		apiProp := mustProp(t, schema, "apiVersion")
		assertEnum(t, apiProp, []string{"ksail.io/v1alpha1"})
	})

	t.Run("distribution enum", func(t *testing.T) {
		t.Parallel()

		cluster := mustNestedProp(t, schema, "spec", "cluster")
		dist := mustMap(t, cluster["properties"], "cluster.properties")
		distProp := mustMap(t, dist["distribution"], "distribution")
		assertEnum(t, distProp, []string{"Vanilla", "K3s", "Talos"})
	})

	t.Run("provider enum", func(t *testing.T) {
		t.Parallel()

		cluster := mustNestedProp(t, schema, "spec", "cluster")
		props := mustMap(t, cluster["properties"], "cluster.properties")
		prov := mustMap(t, props["provider"], "provider")
		assertEnum(t, prov, []string{"Docker", "Hetzner"})
	})

	t.Run("no required fields on nested objects", func(t *testing.T) {
		t.Parallel()

		// The generator clears required on all nested objects (omitzero).
		spec := mustProp(t, schema, "spec")
		if spec["required"] != nil {
			t.Errorf("spec should have no required fields, got %v", spec["required"])
		}
	})
}

func mustProp(t *testing.T, schema map[string]any, key string) map[string]any {
	t.Helper()

	props := mustMap(t, schema["properties"], "properties")

	return mustMap(t, props[key], key)
}

func mustNestedProp(t *testing.T, schema map[string]any, keys ...string) map[string]any {
	t.Helper()

	current := schema
	for _, key := range keys {
		props := mustMap(t, current["properties"], "properties")
		current = mustMap(t, props[key], key)
	}

	return current
}

func mustMap(t *testing.T, v any, path string) map[string]any {
	t.Helper()

	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected %s to be an object, got %T", path, v)
	}

	return m
}

func assertEnum(t *testing.T, prop map[string]any, want []string) {
	t.Helper()

	got, ok := prop["enum"].([]any)
	if !ok {
		t.Fatalf("expected enum to be an array, got %T", prop["enum"])
	}

	if len(got) != len(want) {
		t.Fatalf("enum length = %d, want %d: %v", len(got), len(want), got)
	}

	for i, w := range want {
		if got[i] != w {
			t.Errorf("enum[%d] = %v, want %v", i, got[i], w)
		}
	}
}

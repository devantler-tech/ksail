// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

package schemas_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// Frequently asserted property keys.
const (
	specKey       = "spec"
	clusterKey    = "cluster"
	connectionKey = "connection"
)

// generateSchema runs the schema generator and returns the parsed JSON.
func generateSchema(t *testing.T) map[string]any {
	t.Helper()

	outPath := generateSchemaFile(t)

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

// generateSchemaFile runs the schema generator and returns the output path.
func generateSchemaFile(t *testing.T) string {
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

	// Determine the absolute path to the schemas directory based on this test file's location.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file path")
	}

	schemasDir, err := filepath.Abs(filepath.Dir(thisFile))
	if err != nil {
		t.Fatalf("unable to determine absolute schemas directory: %v", err)
	}

	cmd.Dir = schemasDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generator failed: %v\noutput:\n%s", err, string(out))
	}

	return outPath
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

		// The root must not require spec: the runtime treats an absent spec as
		// all-defaults and the scaffolder emits ksail.yaml without a spec key,
		// so the published schema has to accept the scaffolder's own output.
		if schema["required"] != nil {
			t.Errorf("required = %v, want no required fields at root", schema["required"])
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

		cluster := mustNestedProp(t, schema, specKey, clusterKey)
		dist := mustMap(t, cluster["properties"], "cluster.properties")
		distProp := mustMap(t, dist["distribution"], "distribution")
		assertEnum(t, distProp, []string{"Vanilla", "K3s", "Talos", "VCluster", "KWOK", "EKS"})
	})

	t.Run("provider enum", func(t *testing.T) {
		t.Parallel()

		cluster := mustNestedProp(t, schema, specKey, clusterKey)
		props := mustMap(t, cluster["properties"], "cluster.properties")
		prov := mustMap(t, props["provider"], "provider")
		assertEnum(t, prov, []string{"Docker", "Hetzner", "Omni", "AWS", "Kubernetes"})
	})

	testNoRequiredFields(t, schema)
	testFieldDescriptions(t, schema)
	testMetadataNameConstraints(t, schema)
	testDeprecatedAliases(t, schema)
	testDistributionProviderConstraints(t, schema)
}

// testFieldDescriptions asserts the Go doc comments flowed into descriptions
// for the prominent spec fields.
func testFieldDescriptions(t *testing.T, schema map[string]any) {
	t.Helper()

	t.Run("field descriptions present", func(t *testing.T) {
		t.Parallel()

		paths := [][]string{
			{specKey, clusterKey, "distribution"},
			{specKey, clusterKey, "provider"},
			{specKey, clusterKey, "cni"},
			{specKey, clusterKey, "csi"},
			{specKey, clusterKey, "metricsServer"},
			{specKey, clusterKey, "loadBalancer"},
			{specKey, clusterKey, "gitOpsEngine"},
			{specKey, clusterKey, connectionKey, "kubeconfig"},
			{specKey, clusterKey, connectionKey, "context"},
			{specKey, clusterKey, connectionKey, "timeout"},
		}

		for _, path := range paths {
			prop := mustNestedProp(t, schema, path...)
			if desc, _ := prop["description"].(string); desc == "" {
				t.Errorf("expected a description on %s", strings.Join(path, "."))
			}
		}
	})
}

// testMetadataNameConstraints asserts metadata.name carries the cluster-name
// constraints ValidateClusterName enforces at runtime.
func testMetadataNameConstraints(t *testing.T, schema map[string]any) {
	t.Helper()

	t.Run("metadata name constraints", func(t *testing.T) {
		t.Parallel()

		name := mustNestedProp(t, schema, "metadata", "name")

		if pattern, _ := name["pattern"].(string); pattern == "" {
			t.Error("expected a pattern on metadata.name")
		}

		const wantMaxLength = float64(63)
		if got := name["maxLength"]; got != wantMaxLength {
			t.Errorf("metadata.name maxLength = %v, want %v", got, wantMaxLength)
		}
	})
}

// testDeprecatedAliases asserts the three migration-alias fields are flagged
// deprecated so editors warn at authoring time.
func testDeprecatedAliases(t *testing.T, schema map[string]any) {
	t.Helper()

	t.Run("deprecated alias fields", func(t *testing.T) {
		t.Parallel()

		paths := [][]string{
			{specKey, clusterKey, "nodeAutoscaling"},
			{specKey, clusterKey, "talos", "controlPlanes"},
			{specKey, clusterKey, "talos", "workers"},
		}

		for _, path := range paths {
			prop := mustNestedProp(t, schema, path...)
			if got := prop["deprecated"]; got != true {
				t.Errorf("%s deprecated = %v, want true", strings.Join(path, "."), got)
			}
		}
	})
}

// testDistributionProviderConstraints asserts the allOf/if-then subschemas
// restricting provider per distribution exist and match supportedProviders.
func testDistributionProviderConstraints(t *testing.T, schema map[string]any) {
	t.Helper()

	t.Run("distribution provider constraints", func(t *testing.T) {
		t.Parallel()

		cluster := mustNestedProp(t, schema, specKey, clusterKey)

		allOf, ok := cluster["allOf"].([]any)
		if !ok {
			t.Fatalf("expected spec.cluster allOf to be an array, got %T", cluster["allOf"])
		}

		const wantBranches = 6
		if len(allOf) != wantBranches {
			t.Fatalf("spec.cluster allOf has %d branches, want %d", len(allOf), wantBranches)
		}

		providersByDistribution := collectProviderConstraints(t, allOf)

		assertEnum(t, providersByDistribution["EKS"], []string{"AWS"})
		assertEnum(
			t,
			providersByDistribution["Talos"],
			[]string{"Docker", "Hetzner", "Omni", "Kubernetes"},
		)
	})
}

// collectProviderConstraints maps each if-then branch's distribution const to
// its provider property schema.
func collectProviderConstraints(t *testing.T, allOf []any) map[string]map[string]any {
	t.Helper()

	constraints := make(map[string]map[string]any, len(allOf))

	for index, branch := range allOf {
		branchMap := mustMap(t, branch, "allOf branch")

		ifSchema := mustMap(t, branchMap["if"], "if")
		distribution := mustNestedProp(t, ifSchema, "distribution")

		name, ok := distribution["const"].(string)
		if !ok {
			t.Fatalf(
				"allOf[%d] if distribution const is %T, want string",
				index,
				distribution["const"],
			)
		}

		thenSchema := mustMap(t, branchMap["then"], "then")
		constraints[name] = mustNestedProp(t, thenSchema, "provider")
	}

	return constraints
}

// TestSchemaValidation compiles the generated schema with a real JSON-schema
// validator and exercises representative documents — most importantly the
// scaffolder's own minimal ksail.yaml output (apiVersion + kind, no spec),
// which the published schema must keep accepting.
func TestSchemaValidation(t *testing.T) {
	t.Parallel()

	compiler := jsonschema.NewCompiler()

	sch, err := compiler.Compile(generateSchemaFile(t))
	if err != nil {
		t.Fatalf("compile generated schema: %v", err)
	}

	for _, testCase := range schemaValidationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			instance, err := jsonschema.UnmarshalJSON(strings.NewReader(testCase.instance))
			if err != nil {
				t.Fatalf("unmarshal instance: %v", err)
			}

			err = sch.Validate(instance)
			if testCase.wantValid && err != nil {
				t.Errorf("expected instance to validate, got: %v", err)
			}

			if !testCase.wantValid && err == nil {
				t.Error("expected a validation error, got none")
			}
		})
	}
}

type schemaValidationCase struct {
	name      string
	instance  string
	wantValid bool
}

func schemaValidationCases() []schemaValidationCase {
	return []schemaValidationCase{
		{
			name:      "scaffolded minimal config",
			instance:  `{"apiVersion":"ksail.io/v1alpha1","kind":"Cluster"}`,
			wantValid: true,
		},
		{
			name: "valid distribution provider pair",
			instance: `{"apiVersion":"ksail.io/v1alpha1","kind":"Cluster",` +
				`"metadata":{"name":"my-cluster"},` +
				`"spec":{"cluster":{"distribution":"Talos","provider":"Hetzner"}}}`,
			wantValid: true,
		},
		{
			name: "unsupported distribution provider pair",
			instance: `{"apiVersion":"ksail.io/v1alpha1","kind":"Cluster",` +
				`"spec":{"cluster":{"distribution":"EKS","provider":"Docker"}}}`,
			wantValid: false,
		},
		{
			name: "provider unconstrained when distribution omitted",
			instance: `{"apiVersion":"ksail.io/v1alpha1","kind":"Cluster",` +
				`"spec":{"cluster":{"provider":"AWS"}}}`,
			wantValid: true,
		},
		{
			name: "cluster name violating pattern",
			instance: `{"apiVersion":"ksail.io/v1alpha1","kind":"Cluster",` +
				`"metadata":{"name":"My_Cluster"}}`,
			wantValid: false,
		},
		{
			name: "cluster name too long",
			instance: `{"apiVersion":"ksail.io/v1alpha1","kind":"Cluster",` +
				`"metadata":{"name":"` + strings.Repeat("a", 64) + `"}}`,
			wantValid: false,
		},
	}
}

func testNoRequiredFields(t *testing.T, schema map[string]any) {
	t.Helper()

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

// TestSchemaDescriptionsStripControllerGenMarkers guards the generator's marker-stripping:
// controller-gen markers (e.g. +kubebuilder:validation:XValidation) in a Go doc comment
// steer CRD generation, not this config schema, and must never leak into a user-facing
// description. storageHealthTimeout carries an XValidation marker, so it is the concrete
// regression target; the tree-wide sweep guards every other field too.
func TestSchemaDescriptionsStripControllerGenMarkers(t *testing.T) {
	t.Parallel()

	schema := generateSchema(t)

	field := mustNestedProp(t, schema, specKey, clusterKey, "talos", "storageHealthTimeout")

	desc, _ := field["description"].(string)
	if desc == "" {
		t.Fatal("storageHealthTimeout description is empty")
	}

	if strings.Contains(desc, "+kubebuilder") {
		t.Errorf("storageHealthTimeout description leaks a controller-gen marker:\n%s", desc)
	}

	if got, _ := field["pattern"].(string); got == "" {
		t.Error("storageHealthTimeout lost its duration pattern constraint")
	}

	forEachDescription(schema, func(desc string) {
		if strings.Contains(desc, "+kubebuilder") {
			t.Errorf("a schema description leaks a controller-gen marker:\n%s", desc)
		}
	})
}

// forEachDescription invokes visit on every "description" string in the schema tree.
func forEachDescription(node any, visit func(string)) {
	switch typed := node.(type) {
	case map[string]any:
		if desc, ok := typed["description"].(string); ok {
			visit(desc)
		}

		for _, child := range typed {
			forEachDescription(child, visit)
		}
	case []any:
		for _, child := range typed {
			forEachDescription(child, visit)
		}
	}
}

// Package main provides a CLI tool to generate JSON schema from KSail config types.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/invopop/jsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

const (
	dirPermissions  = 0o750
	filePermissions = 0o600
)

func main() {
	if err := run(os.Stdout, os.Stderr, os.Args); err != nil {
		os.Exit(1)
	}
}

func run(stdout, stderr io.Writer, args []string) error {
	reflector := &jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
		Mapper:                    customTypeMapper,
	}
	schema := reflector.Reflect(&v1alpha1.Cluster{})

	customizeSchema(schema)

	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "Error marshaling schema: %v\n", err)
		return fmt.Errorf("marshal schema: %w", err)
	}

	outputPath := "schemas/ksail-config.schema.json"
	if len(args) > 1 {
		outputPath = args[1]
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermissions); err != nil {
		fmt.Fprintf(stderr, "Error creating directory: %v\n", err)
		return fmt.Errorf("create directory: %w", err)
	}

	if err := os.WriteFile(outputPath, schemaJSON, filePermissions); err != nil {
		fmt.Fprintf(stderr, "Error writing schema: %v\n", err)
		return fmt.Errorf("write schema: %w", err)
	}

	fmt.Fprintf(stdout, "Successfully generated JSON schema at %s\n", outputPath)
	return nil
}

// customizeSchema applies all schema customizations.
func customizeSchema(schema *jsonschema.Schema) {
	schema.ID = ""
	schema.Title = "KSail Cluster Configuration"
	schema.Description = "JSON schema for KSail cluster configuration (ksail.yaml)"

	// Walk schema tree once, applying all transformations
	walkSchema(schema, func(s *jsonschema.Schema) {
		// Clear required (all fields use omitzero)
		s.Required = nil

		// Mark empty objects as alpha placeholders
		if s.Type == "object" && (s.Properties == nil || s.Properties.Len() == 0) {
			if s.Description == "" {
				s.Description = "Alpha placeholder (currently unsupported)."
			}
		}
	})

	// Restore root-level spec requirement
	schema.Required = []string{"spec"}

	// Set kind/apiVersion enums from constants
	if schema.Properties != nil {
		if p, ok := schema.Properties.Get("kind"); ok && p != nil {
			p.Enum = []any{v1alpha1.Kind}
		}
		if p, ok := schema.Properties.Get("apiVersion"); ok && p != nil {
			p.Enum = []any{v1alpha1.APIVersion}
		}
	}
}

// walkSchema traverses the schema tree and calls fn on each node.
func walkSchema(schema *jsonschema.Schema, fn func(*jsonschema.Schema)) {
	if schema == nil {
		return
	}

	fn(schema)

	if schema.Properties != nil {
		for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			walkSchema(pair.Value, fn)
		}
	}
	if schema.Items != nil {
		walkSchema(schema.Items, fn)
	}
	if schema.AdditionalProperties != nil {
		walkSchema(schema.AdditionalProperties, fn)
	}
}

// customTypeMapper provides custom schema mappings for v1alpha1 types.
func customTypeMapper(t reflect.Type) *jsonschema.Schema {
	switch t {
	case reflect.TypeFor[metav1.Duration]():
		return &jsonschema.Schema{
			Type:    "string",
			Pattern: "^[0-9]+(ns|us|µs|ms|s|m|h)$",
		}
	case reflect.TypeFor[v1alpha1.Distribution]():
		return enumSchema(v1alpha1.ValidDistributions())
	case reflect.TypeFor[v1alpha1.CNI]():
		return enumSchema(v1alpha1.ValidCNIs())
	case reflect.TypeFor[v1alpha1.CSI]():
		return enumSchema(v1alpha1.ValidCSIs())
	case reflect.TypeFor[v1alpha1.MetricsServer]():
		return enumSchema(v1alpha1.ValidMetricsServers())
	case reflect.TypeFor[v1alpha1.CertManager]():
		return enumSchema(v1alpha1.ValidCertManagers())
	case reflect.TypeFor[v1alpha1.LocalRegistry]():
		return enumSchema(v1alpha1.ValidLocalRegistryModes())
	case reflect.TypeFor[v1alpha1.GitOpsEngine]():
		return enumSchema(v1alpha1.ValidGitOpsEngines())
	default:
		return nil
	}
}

// enumSchema creates a string enum schema from typed values.
func enumSchema[T ~string](values []T) *jsonschema.Schema {
	enums := make([]any, len(values))
	for i, v := range values {
		enums[i] = string(v)
	}
	return &jsonschema.Schema{Type: "string", Enum: enums}
}

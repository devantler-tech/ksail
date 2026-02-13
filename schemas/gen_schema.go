// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the MIT License.

//go:build ignore

// gen_schema.go generates a JSON schema from KSail config types and writes
// it to ksail-config.schema.json. This replaces the separate Go module that
// previously lived in .github/scripts/generate-schema/.
//
// Usage:
//
//	go run gen_schema.go [output-path]
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/invopop/jsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	dirPermissions  = 0o750
	filePermissions = 0o600
)

func main() {
	if err := run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	reflector := &jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
		Mapper:                    customTypeMapper,
	}
	schema := reflector.Reflect(&v1alpha1.Cluster{})

	customizeSchema(schema)

	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}

	outputPath := "ksail-config.schema.json"
	if len(args) > 1 {
		outputPath = args[1]
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermissions); err != nil {
		return fmt.Errorf("create directory for %s: %w", outputPath, err)
	}

	if err := os.WriteFile(outputPath, schemaJSON, filePermissions); err != nil {
		return fmt.Errorf("write schema to %s: %w", outputPath, err)
	}

	fmt.Printf("gen_schema: wrote %s (%d bytes)\n", outputPath, len(schemaJSON))

	return nil
}

// customizeSchema applies all schema customizations.
func customizeSchema(schema *jsonschema.Schema) {
	schema.ID = ""
	schema.Title = "KSail Cluster Configuration"
	schema.Description = "JSON schema for KSail cluster configuration (ksail.yaml)"

	// Walk schema tree once, applying all transformations.
	walkSchema(schema, func(s *jsonschema.Schema) {
		// Clear required (all fields use omitzero).
		s.Required = nil

		// Mark empty objects as alpha placeholders.
		if s.Type == "object" && (s.Properties == nil || s.Properties.Len() == 0) {
			if s.Description == "" {
				s.Description = "Alpha placeholder (currently unsupported)."
			}
		}
	})

	// Restore root-level spec requirement.
	schema.Required = []string{"spec"}

	// Set kind/apiVersion enums from constants.
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
// It automatically detects enum types that implement the EnumValuer interface.
func customTypeMapper(t reflect.Type) *jsonschema.Schema {
	// Check if this type implements EnumValuer (try pointer receiver first).
	enumValuerType := reflect.TypeFor[v1alpha1.EnumValuer]()
	ptrType := reflect.PointerTo(t)

	if ptrType.Implements(enumValuerType) {
		// Create a pointer to zero value and call ValidValues().
		zero := reflect.New(t)
		values := zero.Interface().(v1alpha1.EnumValuer).ValidValues()

		enumVals := make([]any, len(values))
		for i, v := range values {
			enumVals[i] = v
		}

		return &jsonschema.Schema{Type: "string", Enum: enumVals}
	}

	// Special case for metav1.Duration.
	if t == reflect.TypeFor[metav1.Duration]() {
		return &jsonschema.Schema{
			Type:    "string",
			Pattern: "^[0-9]+(ns|us|µs|ms|s|m|h)$",
		}
	}

	return nil
}

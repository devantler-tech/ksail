// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

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
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/invopop/jsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	dirPermissions  = 0o750
	filePermissions = 0o600

	// v1alpha1SourceDir is the path to the v1alpha1 API package sources, relative
	// to the schemas/ directory the generator always runs from (via go:generate or
	// the package test). Go doc comments extracted from it become schema
	// descriptions.
	v1alpha1SourceDir = "../pkg/apis/cluster/v1alpha1"

	// commentImportBase compensates for the ".." in v1alpha1SourceDir: the comment
	// extractor joins base and each walked file's directory to derive the import
	// path, so "<module>/schemas" + "../pkg/apis/cluster/v1alpha1" cleans to the
	// v1alpha1 package's real import path.
	commentImportBase = "github.com/devantler-tech/ksail/v7/schemas"

	// clusterNamePattern mirrors clusterNameRegex in
	// pkg/apis/cluster/v1alpha1/validation.go (DNS-1123: lowercase alphanumerics
	// and hyphens, starting with a letter, not ending with a hyphen).
	clusterNamePattern = `^[a-z][a-z0-9-]*[a-z0-9]$|^[a-z]$`
)

// errSchemaPathMissing reports that a schema customization targeted a property
// path that no longer exists — a drift guard for the hand-listed paths below.
var errSchemaPathMissing = errors.New("schema property path missing")

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

	// Turn the v1alpha1 Go doc comments into schema descriptions. Struct tags
	// (jsonschema:"description=...") still win where both exist.
	if err := reflector.AddGoComments(commentImportBase, v1alpha1SourceDir); err != nil {
		return fmt.Errorf("extract Go comments from %s: %w", v1alpha1SourceDir, err)
	}

	schema := reflector.Reflect(&v1alpha1.Cluster{})

	if err := customizeSchema(schema); err != nil {
		return err
	}

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
func customizeSchema(schema *jsonschema.Schema) error {
	schema.ID = ""
	schema.Title = "KSail Cluster Configuration"
	schema.Description = "JSON schema for KSail cluster configuration (ksail.yaml)"

	// Walk schema tree once, applying all transformations.
	walkSchema(schema, func(s *jsonschema.Schema) {
		// Clear required everywhere (all fields use omitzero). The root spec is
		// deliberately NOT required: the runtime treats an absent spec as
		// all-defaults, and `ksail cluster init` scaffolds a ksail.yaml without
		// a spec key, so the published schema must accept that output.
		s.Required = nil

		// Mark empty objects as alpha placeholders.
		if s.Type == "object" && (s.Properties == nil || s.Properties.Len() == 0) {
			if s.Description == "" {
				s.Description = "Alpha placeholder (currently unsupported)."
			}
		}
	})

	// The Cluster type carries a Status subresource for the operator, but ksail.yaml never
	// contains status, so it is omitted from the configuration schema.
	if schema.Properties != nil {
		schema.Properties.Delete("status")
	}

	// Set kind/apiVersion enums from constants.
	if schema.Properties != nil {
		if p, ok := schema.Properties.Get("kind"); ok && p != nil {
			p.Enum = []any{v1alpha1.Kind}
		}

		if p, ok := schema.Properties.Get("apiVersion"); ok && p != nil {
			p.Enum = []any{v1alpha1.APIVersion}
		}
	}

	if err := markDeprecatedAliases(schema); err != nil {
		return err
	}

	return addDistributionProviderConstraints(schema)
}

// propertyAt returns the schema node at the given property path, or nil when
// any path segment is missing.
func propertyAt(schema *jsonschema.Schema, path ...string) *jsonschema.Schema {
	current := schema

	for _, key := range path {
		if current == nil || current.Properties == nil {
			return nil
		}

		next, ok := current.Properties.Get(key)
		if !ok {
			return nil
		}

		current = next
	}

	return current
}

// markDeprecatedAliases sets "deprecated": true on the migration-alias fields
// KSail still reads but rewrites on load (see migrateDeprecatedNodeCounts and
// migrateDeprecatedNodeAutoscaling in pkg/fsutil/configmanager/ksail), so
// editors flag them at authoring time.
func markDeprecatedAliases(schema *jsonschema.Schema) error {
	aliasPaths := [][]string{
		{"spec", "cluster", "nodeAutoscaling"},
		{"spec", "cluster", "talos", "controlPlanes"},
		{"spec", "cluster", "talos", "workers"},
	}

	for _, path := range aliasPaths {
		property := propertyAt(schema, path...)
		if property == nil {
			return fmt.Errorf("%w: %s", errSchemaPathMissing, strings.Join(path, "."))
		}

		property.Deprecated = true
	}

	return nil
}

// addDistributionProviderConstraints appends allOf/if-then subschemas to
// spec.cluster restricting provider to the values each distribution supports
// (derived from Provider.ValidateForDistribution, i.e. supportedProviders).
// Editor schema only: the CRD deliberately gains no equivalent validation
// markers — rejecting pre-existing CRs server-side would be a breaking change.
func addDistributionProviderConstraints(schema *jsonschema.Schema) error {
	cluster := propertyAt(schema, "spec", "cluster")
	if cluster == nil {
		return fmt.Errorf("%w: spec.cluster", errSchemaPathMissing)
	}

	for _, distribution := range v1alpha1.ValidDistributions() {
		ifProperties := jsonschema.NewProperties()
		ifProperties.Set("distribution", &jsonschema.Schema{Const: string(distribution)})

		thenProperties := jsonschema.NewProperties()
		thenProperties.Set("provider", &jsonschema.Schema{Enum: supportedProviderValues(distribution)})

		cluster.AllOf = append(cluster.AllOf, &jsonschema.Schema{
			If: &jsonschema.Schema{
				Properties: ifProperties,
				// Without this guard a config that omits distribution would
				// match every if-branch and constrain provider to the
				// intersection of all branches.
				Required: []string{"distribution"},
			},
			Then: &jsonschema.Schema{Properties: thenProperties},
		})
	}

	return nil
}

// supportedProviderValues returns the providers valid for the distribution as
// schema enum values.
func supportedProviderValues(distribution v1alpha1.Distribution) []any {
	providers := make([]any, 0, len(v1alpha1.ValidProviders()))

	for _, provider := range v1alpha1.ValidProviders() {
		if provider.ValidateForDistribution(distribution) == nil {
			providers = append(providers, string(provider))
		}
	}

	return providers
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

// enumValues returns an EnumValuer's valid values as a []any suitable for a
// jsonschema Schema.Enum field.
func enumValues(valuer v1alpha1.EnumValuer) []any {
	values := valuer.ValidValues()

	enumVals := make([]any, len(values))
	for i, v := range values {
		enumVals[i] = v
	}

	return enumVals
}

// customTypeMapper provides custom schema mappings for v1alpha1 types.
// It automatically detects enum types that implement the EnumValuer interface.
func customTypeMapper(t reflect.Type) *jsonschema.Schema {
	// AutoscalerExpanderList accepts either a single expander (scalar) or an
	// ordered priority list, so expose both shapes via oneOf in the schema.
	if t == reflect.TypeFor[v1alpha1.AutoscalerExpanderList]() {
		enumVals := enumValues(new(v1alpha1.AutoscalerExpander))

		return &jsonschema.Schema{
			OneOf: []*jsonschema.Schema{
				{Type: "string", Enum: enumVals},
				{Type: "array", Items: &jsonschema.Schema{Type: "string", Enum: enumVals}},
			},
		}
	}

	// Check if this type implements EnumValuer (try pointer receiver first).
	enumValuerType := reflect.TypeFor[v1alpha1.EnumValuer]()
	ptrType := reflect.PointerTo(t)

	if ptrType.Implements(enumValuerType) {
		// Create a pointer to zero value and call ValidValues().
		zero := reflect.New(t)

		return &jsonschema.Schema{
			Type: "string",
			Enum: enumValues(zero.Interface().(v1alpha1.EnumValuer)),
		}
	}

	// Special case for metav1.Duration.
	if t == reflect.TypeFor[metav1.Duration]() {
		return &jsonschema.Schema{
			Type:    "string",
			Pattern: "^[0-9]+(ns|us|µs|ms|s|m|h)$",
		}
	}

	// Special case for metav1.ObjectMeta. The Cluster embeds full Kubernetes object metadata
	// for the operator/CRD, but ksail.yaml only supports the cluster name, so the configuration
	// schema exposes just metadata.name (matching the previous custom Metadata type).
	if t == reflect.TypeFor[metav1.ObjectMeta]() {
		return objectMetaSchema()
	}

	return nil
}

// objectMetaSchema builds the metadata schema: just the cluster name, carrying
// the same DNS-1123 constraints ValidateClusterName enforces at runtime.
func objectMetaSchema() *jsonschema.Schema {
	maxLength := uint64(v1alpha1.ClusterNameMaxLength)

	props := jsonschema.NewProperties()
	props.Set("name", &jsonschema.Schema{
		Type: "string",
		Description: "Cluster name (DNS-1123 compliant: lowercase letters, numbers, and hyphens; " +
			"must start with a letter and must not end with a hyphen).",
		Pattern:   clusterNamePattern,
		MaxLength: &maxLength,
	})

	return &jsonschema.Schema{
		Type:                 "object",
		Properties:           props,
		AdditionalProperties: jsonschema.FalseSchema,
	}
}

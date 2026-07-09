// Package crdschema derives kubeconform-consumable JSON schemas from
// CustomResourceDefinition manifests found in a GitOps workload tree.
//
// A custom resource whose CRD is not published in the datree CRDs-catalog is
// silently skipped by `ksail workload validate` (kubeconform's
// --ignore-missing-schemas defaults to true). Many GitOps repos, however, vendor
// the CRDs their operators install right alongside the custom resources that use
// them — so the authoritative schema is already present in the tree as
// spec.versions[].schema.openAPIV3Schema. Materialize extracts those schemas and
// writes them in the {group}/{kind}_{version}.json layout kubeconform already uses
// for the catalog, so validate can be pointed at them via a schema location and
// validate the custom resources instead of skipping them.
//
// Extraction is best-effort: a CRD (or an individual version) that cannot be parsed
// or converted is reported as a Warning and skipped, so a single malformed manifest
// never fails a validate run.
package crdschema

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

const (
	// crdKind is the kind of a CustomResourceDefinition manifest.
	crdKind = "CustomResourceDefinition"
	// schemaFilePerm is the permission for a written schema file.
	schemaFilePerm = 0o600
	// schemaDirPerm is the permission for a created schema directory.
	schemaDirPerm = 0o750
	// typeKey is the JSON Schema keyword whose value names a node's type.
	typeKey = "type"
)

// SchemaLocation returns the kubeconform schema-location string for schemas
// materialized under destDir. It resolves the {group}/{kind}_{version}.json layout
// Materialize writes, mirroring the datree CRDs-catalog convention kubeconform
// already understands, so it can be appended to a validator's schema locations.
func SchemaLocation(destDir string) string {
	return filepath.Join(destDir, "{{.Group}}", "{{.ResourceKind}}_{{.ResourceAPIVersion}}.json")
}

// typeMeta is the minimal head of a manifest, used to detect CRD documents without
// fully unmarshalling every resource in the tree.
type typeMeta struct {
	Kind string `json:"kind"`
}

// Warning is a non-fatal problem encountered while deriving a schema. The affected
// CRD (or version) is skipped rather than failing the caller, giving graceful
// degradation when a manifest is malformed or uses an unsupported shape.
type Warning struct {
	// Source is the file the CRD was read from.
	Source string
	// CRD is the CRD metadata.name, or "" when it could not be determined.
	CRD string
	// Reason explains why a schema could not be derived.
	Reason string
}

// String renders a Warning as "<crd> (<source>): <reason>".
func (w Warning) String() string {
	crd := w.CRD
	if crd == "" {
		crd = "<unnamed>"
	}

	return fmt.Sprintf("%s (%s): %s", crd, w.Source, w.Reason)
}

// Result reports the outcome of Materialize.
type Result struct {
	// Written is the number of per-version JSON schema files written under destDir.
	Written int
	// Warnings lists the CRDs (or versions) that were skipped, with a reason each.
	Warnings []Warning
}

// versionSchema is one CRD version rendered as a kubeconform JSON schema.
type versionSchema struct {
	group   string
	kind    string // lowercased, to match kubeconform's {{.ResourceKind}} convention
	version string
	schema  []byte
}

// Materialize walks root for CustomResourceDefinition manifests, converts each
// version's openAPIV3Schema into a kubeconform-consumable JSON schema, and writes
// them under destDir laid out as {group}/{lowercased-kind}_{version}.json (the
// layout SchemaLocation resolves). It returns the number of schema files written
// and any per-CRD warnings.
//
// A CRD (or a single version) that cannot be parsed or converted is skipped with a
// Warning rather than failing, so a malformed manifest never breaks validation. An
// error is returned only when the tree cannot be walked or a schema cannot be
// written to destDir.
func Materialize(root, destDir string) (Result, error) {
	files, err := yamlFiles(root)
	if err != nil {
		return Result{}, err
	}

	var result Result

	for _, file := range files {
		// #nosec G304 -- file path comes from walking the caller-supplied root
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			result.Warnings = append(result.Warnings, Warning{
				Source: file,
				Reason: "read file: " + readErr.Error(),
			})

			continue
		}

		writeErr := materializeFile(data, file, destDir, &result)
		if writeErr != nil {
			return result, writeErr
		}
	}

	return result, nil
}

// MaterializeBytes converts every CustomResourceDefinition document found in a
// single pre-rendered manifest stream (e.g. Helm/Kustomize rendered output, not
// necessarily present verbatim anywhere in the source tree) into kubeconform
// schemas under destDir, using the same {group}/{kind}_{version}.json layout as
// Materialize. It complements Materialize, which only discovers CRDs by walking
// a raw file tree: a chart that ships its CRDs under templates/, or a kustomize
// component that generates one, is invisible to that walk but present in
// rendered output. source is used only to attribute warnings (e.g. the
// kustomization directory the manifest stream was rendered from).
//
// Extraction is best-effort, mirroring Materialize: a CRD (or a single version)
// that cannot be parsed or converted is reported as a Warning and skipped rather
// than failing the caller. An error is returned only when a schema cannot be
// written to destDir.
func MaterializeBytes(data []byte, source, destDir string) (Result, error) {
	var result Result

	err := materializeFile(data, source, destDir, &result)
	if err != nil {
		return result, err
	}

	return result, nil
}

// materializeFile extracts and writes every CRD schema found in a single file's
// documents, appending warnings to result. It returns an error only when a schema
// cannot be written to destDir.
func materializeFile(data []byte, source, destDir string, result *Result) error {
	for _, doc := range fsutil.SplitYAMLDocuments(data) {
		if !isCRD(doc) {
			continue
		}

		name, schemas, warnings := schemasFromCRD(doc)
		for _, warning := range warnings {
			warning.Source = source
			warning.CRD = name
			result.Warnings = append(result.Warnings, warning)
		}

		for _, schema := range schemas {
			err := writeSchema(destDir, schema)
			if err != nil {
				return err
			}

			result.Written++
		}
	}

	return nil
}

// isCRD reports whether a YAML document declares a CustomResourceDefinition.
func isCRD(doc []byte) bool {
	var meta typeMeta

	err := yaml.Unmarshal(doc, &meta)
	if err != nil {
		return false
	}

	return meta.Kind == crdKind
}

// schemasFromCRD parses a CRD document and returns its name, one JSON schema per
// version that carries an openAPIV3Schema, and warnings for versions (or the whole
// CRD) that could not be converted.
func schemasFromCRD(doc []byte) (string, []versionSchema, []Warning) {
	var crd apiextensionsv1.CustomResourceDefinition

	err := yaml.Unmarshal(doc, &crd)
	if err != nil {
		return "", nil, []Warning{{Reason: "parse CRD: " + err.Error()}}
	}

	group := crd.Spec.Group
	kind := crd.Spec.Names.Kind

	if group == "" || kind == "" {
		return crd.Name, nil, []Warning{{Reason: "CRD is missing spec.group or spec.names.kind"}}
	}

	if unsafePathComponent(group) || unsafePathComponent(kind) {
		return crd.Name, nil, []Warning{{
			Reason: fmt.Sprintf(
				"spec.group %q or spec.names.kind %q is not a safe path component", group, kind),
		}}
	}

	var (
		schemas  []versionSchema
		warnings []Warning
	)

	for _, version := range crd.Spec.Versions {
		schema, warning := schemaForVersion(group, kind, version)
		if warning != nil {
			warnings = append(warnings, *warning)

			continue
		}

		schemas = append(schemas, *schema)
	}

	return crd.Name, schemas, warnings
}

// schemaForVersion converts a single CRD version into a kubeconform schema. It
// returns exactly one of a schema or a Warning: a Warning when the version name is
// not a safe path component, carries no openAPIV3Schema, or cannot be converted.
func schemaForVersion(
	group, kind string,
	version apiextensionsv1.CustomResourceDefinitionVersion,
) (*versionSchema, *Warning) {
	if unsafePathComponent(version.Name) {
		return nil, &Warning{
			Reason: fmt.Sprintf("version %q is not a safe path component", version.Name),
		}
	}

	if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
		return nil, &Warning{
			Reason: fmt.Sprintf("version %q has no openAPIV3Schema", version.Name),
		}
	}

	schema, convErr := convertSchema(version.Schema.OpenAPIV3Schema)
	if convErr != nil {
		return nil, &Warning{
			Reason: fmt.Sprintf("version %q: %s", version.Name, convErr.Error()),
		}
	}

	return &versionSchema{
		group:   group,
		kind:    strings.ToLower(kind),
		version: version.Name,
		schema:  schema,
	}, nil
}

// convertSchema serializes a CRD openAPIV3Schema into a kubeconform-consumable JSON
// schema. The structural schema already describes the custom-resource object; the
// standard apiVersion/kind/metadata object fields are injected when the CRD omits
// them (Kubernetes supplies these implicitly, so CRD authors rarely declare them)
// so kubeconform does not reject them. Unknown OpenAPI keywords (x-kubernetes-*,
// nullable, …) are carried through verbatim — kubeconform's JSON-schema validator
// ignores keywords it does not recognize.
func convertSchema(props *apiextensionsv1.JSONSchemaProps) ([]byte, error) {
	raw, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("marshal openAPIV3Schema: %w", err)
	}

	var schema map[string]any

	err = json.Unmarshal(raw, &schema)
	if err != nil {
		return nil, fmt.Errorf("decode openAPIV3Schema: %w", err)
	}

	if schema[typeKey] == nil {
		schema[typeKey] = "object"
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		properties = map[string]any{}
		schema["properties"] = properties
	}

	injectField(properties, "apiVersion", map[string]any{typeKey: "string"})
	injectField(properties, "kind", map[string]any{typeKey: "string"})
	injectField(properties, "metadata", map[string]any{typeKey: "object"})

	out, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode schema: %w", err)
	}

	return out, nil
}

// injectField sets props[name] = def only when name is absent, so a CRD that does
// declare the field keeps its own (possibly stricter) definition.
func injectField(props map[string]any, name string, def map[string]any) {
	if _, ok := props[name]; !ok {
		props[name] = def
	}
}

// unsafePathComponent reports whether s cannot be used verbatim as a single path
// element — it is empty, a current/parent-directory reference, or contains a path
// separator. schemasFromCRD rejects a CRD whose group/kind/version trips this so a
// crafted manifest cannot make writeSchema traverse outside destDir. Valid
// Kubernetes group/kind/version names never contain these, so real CRDs pass.
func unsafePathComponent(s string) bool {
	return s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`)
}

// writeSchema writes one version's schema under destDir as
// {group}/{kind}_{version}.json, creating the group directory as needed.
func writeSchema(destDir string, schema versionSchema) error {
	dir := filepath.Join(destDir, schema.group)

	err := os.MkdirAll(dir, schemaDirPerm)
	if err != nil {
		return fmt.Errorf("create schema directory %s: %w", dir, err)
	}

	file := filepath.Join(dir, fmt.Sprintf("%s_%s.json", schema.kind, schema.version))

	err = os.WriteFile(file, schema.schema, schemaFilePerm)
	if err != nil {
		return fmt.Errorf("write schema %s: %w", file, err)
	}

	return nil
}

// yamlFiles returns every .yaml/.yml file under root (which may be a single file).
func yamlFiles(root string) ([]string, error) {
	files, err := fsutil.WalkFiles(root, func(path string, _ fs.DirEntry) (bool, error) {
		ext := strings.ToLower(filepath.Ext(path))

		return ext == ".yaml" || ext == ".yml", nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect YAML files: %w", err)
	}

	return files, nil
}

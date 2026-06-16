package fluxsubst

// This file exposes the engine's internal schema-introspection and
// type-coercion helpers as exported functions so that command-package tests
// (which historically reached them through unexported re-export shims) keep a
// stable seam after the engine moved here. They are pure helpers with no side
// effects beyond reading the on-disk schema cache and are safe to call
// directly; the package's own tests use the unexported forms.

// GetSchemaTypeAtPath walks a JSON schema following a path like "/spec/replicas"
// and returns the type of the field ("string", "integer", "number", "boolean"),
// or "" when the schema is nil, the path is empty, or the type cannot be resolved.
func GetSchemaTypeAtPath(schema map[string]any, path string) string {
	return getSchemaTypeAtPath(schema, path)
}

// SchemaURLs returns the candidate schema URLs for a given apiVersion/kind.
func SchemaURLs(apiVersion, kind string) []string {
	return schemaURLs(apiVersion, kind)
}

// SplitAPIVersion splits "apps/v1" into ("apps", "v1") or "v1" into ("", "v1").
func SplitAPIVersion(apiVersion string) (string, string) {
	return splitAPIVersion(apiVersion)
}

// TypedPlaceholderValue returns a Go value matching the given JSON schema type
// ("integer"→0, "number"→0.0, "boolean"→true, otherwise the string placeholder).
func TypedPlaceholderValue(schemaType string) any {
	return typedPlaceholderValue(schemaType)
}

// ParseInteger parses trimmed as an integer, returning defaultVal on failure.
func ParseInteger(trimmed, defaultVal string) any {
	return parseInteger(trimmed, defaultVal)
}

// ParseNumber parses trimmed as a floating-point number, returning defaultVal on failure.
func ParseNumber(trimmed, defaultVal string) any {
	return parseNumber(trimmed, defaultVal)
}

// ParseBoolean parses trimmed as a boolean, returning defaultVal on failure.
func ParseBoolean(trimmed, defaultVal string) any {
	return parseBoolean(trimmed, defaultVal)
}

// InferYAMLType uses YAML-native type inference on trimmed, returning defaultVal on failure.
func InferYAMLType(trimmed, defaultVal string) any {
	return inferYAMLType(trimmed, defaultVal)
}

// ParseJSONSchema parses raw JSON bytes into a schema map, or nil on failure.
func ParseJSONSchema(data []byte) map[string]any {
	return parseJSONSchema(data)
}

// SchemaCacheDir returns the on-disk schema cache directory.
func SchemaCacheDir() string {
	return schemaCacheDir()
}

// SchemaCacheFileName produces a deterministic cache filename for a schema URL.
func SchemaCacheFileName(schemaURL string) string {
	return schemaCacheFileName(schemaURL)
}

// SchemaNodeType extracts the declared type from a JSON schema node.
func SchemaNodeType(schema map[string]any) string {
	return schemaNodeType(schema)
}

// IsNumericIndex reports whether str is a non-empty run of ASCII digits.
func IsNumericIndex(str string) bool {
	return isNumericIndex(str)
}

// ResolveFromProperties navigates into a schema's "properties" map for key.
func ResolveFromProperties(schema map[string]any, key string) map[string]any {
	return resolveFromProperties(schema, key)
}

// ResolveFromItems navigates into a schema's "items" for a numeric-index key.
func ResolveFromItems(schema map[string]any, key string) map[string]any {
	return resolveFromItems(schema, key)
}

// ResolveFromCombiners navigates a schema's allOf/anyOf/oneOf combiners for key.
func ResolveFromCombiners(schema map[string]any, key string) map[string]any {
	return resolveFromCombiners(schema, key)
}

package workload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

// fluxVarPattern matches Flux postBuild variable references:
// ${VAR}, ${VAR:-default}, and ${VAR:=default}.
// Groups: 1 = variable name, 2 = operator (:- or :=), 3 = default value.
var fluxVarPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?:(:-|:=)([^}]*))?\}`)

const (
	schemaHTTPTimeout       = 10 * time.Second
	schemaCacheDirPerms     = 0o700
	schemaCacheFileMode     = 0o600
	schemaCacheFileMaxChars = 200

	placeholderString = "placeholder"
)

// schemaRegistry provides thread-safe caching of parsed JSON schemas keyed by "apiVersion/kind".
type schemaRegistry struct {
	cache sync.Map
}

var schemas = &schemaRegistry{} //nolint:gochecknoglobals // singleton schema cache for validation lifecycle

// expandFluxSubstitutions expands Flux postBuild variable references in YAML
// data using type-aware placeholders derived from JSON schemas.
//
// For each YAML document:
//   - ${VAR:-default} / ${VAR:=default} → uses the default value
//   - ${VAR} as entire scalar value → looks up the expected JSON schema type
//     and substitutes a typed placeholder ("placeholder", 0, or true)
//   - ${VAR} mixed with other text → substitutes "placeholder" (string context)
//
// Falls back to regex-based string placeholder expansion when YAML parsing fails.
func expandFluxSubstitutions(ctx context.Context, data []byte) []byte {
	if !fluxVarPattern.Match(data) {
		return data
	}

	docs := splitYAMLDocuments(data)
	if len(docs) == 0 {
		return data
	}

	expanded := make([][]byte, 0, len(docs))
	for _, doc := range docs {
		expanded = append(expanded, expandDocument(ctx, doc))
	}

	return bytes.Join(expanded, []byte("\n---\n"))
}

// splitYAMLDocuments splits multi-document YAML by "---" separators.
func splitYAMLDocuments(data []byte) [][]byte {
	// Split on document separator lines
	parts := bytes.Split(data, []byte("\n---\n"))
	if len(parts) == 0 {
		return [][]byte{data}
	}

	// Also handle leading "---\n"
	result := make([][]byte, 0, len(parts))

	for _, part := range parts {
		trimmed := bytes.TrimSpace(part)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("---")) {
			continue
		}

		result = append(result, part)
	}

	if len(result) == 0 {
		return [][]byte{data}
	}

	return result
}

// expandDocument expands variable references in a single YAML document.
func expandDocument(ctx context.Context, doc []byte) []byte {
	if !fluxVarPattern.Match(doc) {
		return doc
	}

	var obj map[string]any

	err := yaml.Unmarshal(doc, &obj)
	if err != nil {
		return expandFallback(doc)
	}

	apiVersion, _ := obj["apiVersion"].(string)
	kind, _ := obj["kind"].(string)
	schema := schemas.load(ctx, apiVersion, kind)

	walkAndExpand(obj, "", schema)

	out, err := yaml.Marshal(obj)
	if err != nil {
		return expandFallback(doc)
	}

	return out
}

// expandFallback performs simple regex-based expansion when YAML parsing fails.
// All variable references are replaced with string placeholders.
func expandFallback(data []byte) []byte {
	return fluxVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := fluxVarPattern.FindSubmatch(match)
		if len(groups) > 3 && len(groups[3]) > 0 {
			return groups[3] // use default value
		}

		return []byte(placeholderString)
	})
}

// walkAndExpand recursively walks the parsed YAML structure and expands variable references.
func walkAndExpand(obj any, path string, schema map[string]any) any {
	switch val := obj.(type) {
	case map[string]any:
		for key, child := range val {
			val[key] = walkAndExpand(child, path+"/"+key, schema)
		}

		return val
	case []any:
		for idx, item := range val {
			val[idx] = walkAndExpand(item, fmt.Sprintf("%s/%d", path, idx), schema)
		}

		return val
	case string:
		return expandStringValue(val, path, schema)
	default:
		return obj
	}
}

// expandStringValue expands Flux variable references in a string value.
func expandStringValue(val, path string, schema map[string]any) any {
	if !fluxVarPattern.MatchString(val) {
		return val
	}

	// Check if the entire value is a single substitution (bare or with default)
	if match := fluxVarPattern.FindStringSubmatch(val); match != nil && match[0] == val {
		if match[2] == "" {
			// Bare ${VAR} — use type-aware placeholder
			schemaType := getSchemaTypeAtPath(schema, path)

			return typedPlaceholderValue(schemaType)
		}

		// ${VAR:=default} or ${VAR:-default} — parse default as typed value
		schemaType := getSchemaTypeAtPath(schema, path)

		return parseTypedDefault(match[3], schemaType)
	}

	// Mixed text — expand inline (always string context)
	return fluxVarPattern.ReplaceAllStringFunc(val, func(match string) string {
		groups := fluxVarPattern.FindStringSubmatch(match)
		if len(groups) > 3 && groups[3] != "" {
			return groups[3] // use default value
		}

		return placeholderString
	})
}

// typedPlaceholderValue returns a Go value matching the schema type.
// When marshaled by sigs.k8s.io/yaml, these produce correctly typed YAML scalars.
func typedPlaceholderValue(schemaType string) any {
	switch schemaType {
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return true
	default:
		return placeholderString
	}
}

// parseTypedDefault parses a default value string into the appropriate Go type
// based on the schema type, so that sigs.k8s.io/yaml marshals it without quotes.
func parseTypedDefault(defaultVal, schemaType string) any {
	switch schemaType {
	case "integer":
		var intVal int64

		_, err := fmt.Sscanf(defaultVal, "%d", &intVal)
		if err == nil {
			return intVal
		}
	case "number":
		var floatVal float64

		_, err := fmt.Sscanf(defaultVal, "%f", &floatVal)
		if err == nil {
			return floatVal
		}
	case "boolean":
		if defaultVal == "true" {
			return true
		}

		if defaultVal == "false" {
			return false
		}
	}

	return defaultVal
}

// load returns the JSON schema for a Kubernetes resource, or nil if unavailable.
func (reg *schemaRegistry) load(ctx context.Context, apiVersion, kind string) map[string]any {
	if apiVersion == "" || kind == "" {
		return nil
	}

	cacheKey := apiVersion + "/" + kind

	if cached, ok := reg.cache.Load(cacheKey); ok {
		schema, _ := cached.(map[string]any)

		return schema
	}

	schema := fetchSchemaFromDisk(apiVersion, kind)
	if schema == nil {
		schema = fetchSchemaFromNetwork(ctx, apiVersion, kind)
	}

	reg.cache.Store(cacheKey, schema)

	return schema
}

// fetchSchemaFromDisk tries to load a schema from the disk cache.
func fetchSchemaFromDisk(apiVersion, kind string) map[string]any {
	cacheDir := schemaCacheDir()

	for _, schemaURL := range schemaURLs(apiVersion, kind) {
		cachedPath := filepath.Join(cacheDir, schemaCacheFileName(schemaURL))

		data, err := os.ReadFile(cachedPath) //nolint:gosec // controlled cache directory
		if err != nil {
			continue
		}

		schema := parseJSONSchema(data)
		if schema != nil {
			return schema
		}
	}

	return nil
}

// fetchSchemaFromNetwork downloads a schema from the remote URL.
func fetchSchemaFromNetwork(ctx context.Context, apiVersion, kind string) map[string]any {
	client := &http.Client{Timeout: schemaHTTPTimeout}

	for _, schemaURL := range schemaURLs(apiVersion, kind) {
		schema := downloadSchema(ctx, client, schemaURL)
		if schema != nil {
			return schema
		}
	}

	return nil
}

// downloadSchema fetches and parses a single schema URL.
func downloadSchema(ctx context.Context, client *http.Client, schemaURL string) map[string]any {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, schemaURL, nil)
	if err != nil {
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	schema := parseJSONSchema(data)
	if schema != nil {
		writeSchemaCache(schemaURL, data)
	}

	return schema
}

// writeSchemaCache writes a schema to the disk cache.
func writeSchemaCache(schemaURL string, data []byte) {
	cacheDir := schemaCacheDir()

	_ = os.MkdirAll(cacheDir, schemaCacheDirPerms)
	_ = os.WriteFile(
		filepath.Join(cacheDir, schemaCacheFileName(schemaURL)),
		data,
		schemaCacheFileMode,
	)
}

// schemaCacheDir returns the schema cache directory.
func schemaCacheDir() string {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ksail", "kubeconform")
	}

	return filepath.Join(userCacheDir, "ksail", "kubeconform")
}

// schemaCacheFileName produces a deterministic filename for caching a schema URL.
func schemaCacheFileName(schemaURL string) string {
	safe := strings.NewReplacer(
		"://", "_",
		"/", "_",
		".", "_",
	).Replace(schemaURL) + ".json"

	if len(safe) > schemaCacheFileMaxChars {
		safe = safe[len(safe)-schemaCacheFileMaxChars:]
	}

	return safe
}

// schemaURLs returns the candidate schema URLs for a given apiVersion/kind.
func schemaURLs(apiVersion, kind string) []string {
	kindLower := strings.ToLower(kind)
	group, version := splitAPIVersion(apiVersion)

	if group != "" {
		// Try Kubernetes JSON schema first (for core API groups like apps, batch, etc.),
		// then fall back to CRDs catalog for custom resources.
		return []string{
			fmt.Sprintf(
				"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/master-standalone/%s-%s-%s.json",
				kindLower,
				group,
				version,
			),
			fmt.Sprintf(
				"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/%s/%s_%s.json",
				group, kindLower, version,
			),
		}
	}

	return []string{
		fmt.Sprintf(
			"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/master-standalone/%s-%s.json",
			kindLower,
			version,
		),
	}
}

// splitAPIVersion splits "apps/v1" into ("apps", "v1") or "v1" into ("", "v1").
func splitAPIVersion(apiVersion string) (string, string) {
	parts := strings.SplitN(apiVersion, "/", 2) //nolint:mnd // splitting group/version
	if len(parts) == 2 {                        //nolint:mnd // group/version pair
		return parts[0], parts[1]
	}

	return "", parts[0]
}

// parseJSONSchema parses raw JSON bytes into a schema map.
func parseJSONSchema(data []byte) map[string]any {
	var schema map[string]any

	err := json.Unmarshal(data, &schema)
	if err != nil {
		return nil
	}

	return schema
}

// getSchemaTypeAtPath walks a JSON schema following a path like "/spec/replicas"
// and returns the type of the field ("string", "integer", "number", "boolean").
// Returns "string" as fallback when the path or type cannot be resolved.
func getSchemaTypeAtPath(schema map[string]any, path string) string {
	if schema == nil || path == "" {
		return typeString
	}

	trimmed := strings.TrimPrefix(path, "/")
	segments := strings.Split(trimmed, "/")
	current := schema

	for _, seg := range segments {
		current = resolveSchemaNode(current, seg)
		if current == nil {
			return typeString
		}
	}

	return schemaNodeType(current)
}

const typeString = "string"

// resolveSchemaNode navigates one level deeper into a JSON schema for a given key.
func resolveSchemaNode(schema map[string]any, key string) map[string]any {
	if result := resolveFromProperties(schema, key); result != nil {
		return result
	}

	if result := resolveFromItems(schema, key); result != nil {
		return result
	}

	return resolveFromCombiners(schema, key)
}

func resolveFromProperties(schema map[string]any, key string) map[string]any {
	props, found := schema["properties"].(map[string]any)
	if !found {
		return nil
	}

	child, childFound := props[key].(map[string]any)
	if !childFound {
		return nil
	}

	return child
}

func resolveFromItems(schema map[string]any, key string) map[string]any {
	items, ok := schema["items"].(map[string]any)
	if !ok {
		return nil
	}

	if isNumericIndex(key) {
		return items
	}

	return nil
}

func resolveFromCombiners(schema map[string]any, key string) map[string]any {
	for _, combinerKey := range []string{"allOf", "anyOf", "oneOf"} {
		arr, ok := schema[combinerKey].([]any)
		if !ok {
			continue
		}

		for _, entry := range arr {
			sub, ok := entry.(map[string]any)
			if !ok {
				continue
			}

			if result := resolveSchemaNode(sub, key); result != nil {
				return result
			}
		}
	}

	return nil
}

// schemaNodeType extracts the type from a JSON schema node.
func schemaNodeType(schema map[string]any) string {
	if typeVal, ok := schema["type"].(string); ok {
		return typeVal
	}

	if arr, ok := schema["type"].([]any); ok {
		for _, item := range arr {
			if typeVal, ok := item.(string); ok && typeVal != "null" {
				return typeVal
			}
		}
	}

	return typeString
}

// isNumericIndex checks if a string represents a numeric array index.
func isNumericIndex(str string) bool {
	if len(str) == 0 {
		return false
	}

	for _, char := range str {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}

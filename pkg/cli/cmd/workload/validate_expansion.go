package workload

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	yamlio "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// fluxVarPattern matches Flux postBuild variable references:
// ${VAR}, ${VAR:-default}, and ${VAR:=default}.
// Groups: 1 = variable name, 2 = operator (:- or :=), 3 = default value.
var fluxVarPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?:(:-|:=)([^}]*))?\}`)

const (
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
func expandFluxSubstitutions(data []byte) []byte {
	if !fluxVarPattern.Match(data) {
		return data
	}

	docs := splitYAMLDocuments(data)
	if len(docs) == 0 {
		return data
	}

	expanded := make([][]byte, 0, len(docs))
	for _, doc := range docs {
		expanded = append(expanded, expandDocument(doc))
	}

	return bytes.Join(expanded, []byte("\n---\n"))
}

// splitYAMLDocuments splits multi-document YAML using a YAML-aware reader
// that correctly handles document separators ("---") regardless of position,
// trailing whitespace, or carriage returns.
func splitYAMLDocuments(data []byte) [][]byte {
	reader := yamlio.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	var docs [][]byte

	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return [][]byte{data}
		}

		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		docs = append(docs, doc)
	}

	if len(docs) == 0 {
		return [][]byte{data}
	}

	return docs
}

// expandDocument expands variable references in a single YAML document.
func expandDocument(doc []byte) []byte {
	if !fluxVarPattern.Match(doc) {
		return doc
	}

	var obj any

	err := yaml.Unmarshal(doc, &obj)
	if err != nil {
		return expandFallback(doc)
	}

	switch typedObj := obj.(type) {
	case map[string]any:
		return expandMapDocument(typedObj, doc)
	case []any:
		return expandListDocument(typedObj, doc)
	default:
		return expandFallback(doc)
	}
}

// expandMapDocument expands variable references in a YAML document with a map root.
func expandMapDocument(obj map[string]any, doc []byte) []byte {
	apiVersion, _ := obj["apiVersion"].(string)
	kind, _ := obj["kind"].(string)
	schema := schemas.load(apiVersion, kind)

	walkAndExpand(obj, "", schema)

	out, err := yaml.Marshal(obj)
	if err != nil {
		return expandFallback(doc)
	}

	return out
}

// expandListDocument expands variable references in a YAML document with a list root
// (e.g., JSON6902 patch list). There is no single apiVersion/kind,
// so map elements are walked with a nil schema.
func expandListDocument(list []any, doc []byte) []byte {
	for idx, elem := range list {
		if mapElem, isMap := elem.(map[string]any); isMap {
			walkAndExpand(mapElem, "", nil)
			list[idx] = mapElem
		}
	}

	out, err := yaml.Marshal(list)
	if err != nil {
		return expandFallback(doc)
	}

	return out
}

// expandFallback performs simple regex-based expansion when YAML parsing fails.
// Variable references are expanded using env vars with defaults fallback.
func expandFallback(data []byte) []byte {
	return fluxVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := fluxVarPattern.FindSubmatch(match)
		if len(groups) < 4 { //nolint:mnd // regex groups: full, name, op, default
			return match
		}

		return []byte(resolveInlineVar(string(groups[1]), string(groups[2]), string(groups[3])))
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
	match := fluxVarPattern.FindStringSubmatch(val)
	if match != nil && match[0] == val {
		return expandSingleVar(match, path, schema)
	}

	// Mixed text — expand inline (always string context)
	return expandMixedText(val)
}

// expandSingleVar expands a value that consists entirely of a single variable reference.
func expandSingleVar(match []string, path string, schema map[string]any) any {
	varName := match[1]
	operator := match[2]
	defaultVal := match[3]
	schemaType := getSchemaTypeAtPath(schema, path)

	if operator == "" {
		return expandBareVar(varName, schemaType)
	}

	return expandVarWithDefault(varName, defaultVal, operator, schemaType)
}

// expandBareVar expands a bare ${VAR} reference using env vars or typed placeholders.
func expandBareVar(varName, schemaType string) any {
	if envVal, envSet := os.LookupEnv(varName); envSet {
		return parseTypedDefault(envVal, schemaType)
	}

	return typedPlaceholderValue(schemaType)
}

// expandVarWithDefault expands ${VAR:=default} or ${VAR:-default} references.
func expandVarWithDefault(varName, defaultVal, operator, schemaType string) any {
	envVal, envSet := os.LookupEnv(varName)

	switch operator {
	case ":=":
		if envSet {
			return parseTypedDefault(envVal, schemaType)
		}
	case ":-":
		if envSet && envVal != "" {
			return parseTypedDefault(envVal, schemaType)
		}
	}

	return parseTypedDefault(defaultVal, schemaType)
}

// expandMixedText expands variable references embedded within other text (always string context).
func expandMixedText(val string) string {
	return fluxVarPattern.ReplaceAllStringFunc(val, func(match string) string {
		groups := fluxVarPattern.FindStringSubmatch(match)
		if len(groups) < 4 { //nolint:mnd // regex groups: full, name, op, default
			return match
		}

		return resolveInlineVar(groups[1], groups[2], groups[3])
	})
}

// resolveInlineVar resolves a single variable reference in a mixed-text context to a string.
func resolveInlineVar(varName, operator, defaultVal string) string {
	envVal, envSet := os.LookupEnv(varName)

	switch operator {
	case "":
		if envSet {
			return envVal
		}

		return placeholderString
	case ":=":
		if envSet {
			return envVal
		}

		return defaultVal
	case ":-":
		if envSet && envVal != "" {
			return envVal
		}

		return defaultVal
	default:
		return "${" + varName + operator + defaultVal + "}"
	}
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
// When the schema type is unknown (empty string), YAML-native type inference is
// used, matching Flux's behavior where substitution occurs at the text level.
func parseTypedDefault(defaultVal, schemaType string) any {
	trimmed := strings.TrimSpace(defaultVal)

	switch schemaType {
	case "integer":
		return parseInteger(trimmed, defaultVal)
	case "number":
		return parseNumber(trimmed, defaultVal)
	case "boolean":
		return parseBoolean(trimmed, defaultVal)
	case typeString:
		return defaultVal
	default:
		return inferYAMLType(trimmed, defaultVal)
	}
}

func parseInteger(trimmed, defaultVal string) any {
	var intVal int64

	_, err := fmt.Sscanf(trimmed, "%d", &intVal)
	if err == nil {
		return intVal
	}

	return defaultVal
}

func parseNumber(trimmed, defaultVal string) any {
	var floatVal float64

	_, err := fmt.Sscanf(trimmed, "%f", &floatVal)
	if err == nil {
		return floatVal
	}

	return defaultVal
}

func parseBoolean(trimmed, defaultVal string) any {
	if trimmed == "true" {
		return true
	}

	if trimmed == "false" {
		return false
	}

	return defaultVal
}

// inferYAMLType uses YAML-native type inference so that values like "2" become
// integers and "true" becomes a boolean, matching how YAML would parse the
// substituted text.
func inferYAMLType(trimmed, defaultVal string) any {
	var typedVal any

	err := yaml.Unmarshal([]byte(trimmed), &typedVal)
	if err == nil && typedVal != nil {
		return typedVal
	}

	return defaultVal
}

// load returns the JSON schema for a Kubernetes resource from disk cache, or nil if unavailable.
// Network fetching is intentionally omitted to keep validation fast, deterministic, and
// offline-friendly. Schemas are available if kubeconform has previously cached them on disk.
func (reg *schemaRegistry) load(apiVersion, kind string) map[string]any {
	if apiVersion == "" || kind == "" {
		return nil
	}

	cacheKey := apiVersion + "/" + kind

	if cached, ok := reg.cache.Load(cacheKey); ok {
		schema, _ := cached.(map[string]any)

		return schema
	}

	schema := fetchSchemaFromDisk(apiVersion, kind)

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
// Returns empty string when the schema is nil, path is empty, or type cannot be resolved.
func getSchemaTypeAtPath(schema map[string]any, path string) string {
	if schema == nil || path == "" {
		return ""
	}

	trimmed := strings.TrimPrefix(path, "/")
	segments := strings.Split(trimmed, "/")
	current := schema

	for _, seg := range segments {
		current = resolveSchemaNode(current, seg)
		if current == nil {
			return ""
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

	return ""
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

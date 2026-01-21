package v1alpha1

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MarshalYAML trims default values before emitting YAML.
func (c Cluster) MarshalYAML() (any, error) {
	pruned := pruneClusterDefaults(c)

	return structToMap(reflect.ValueOf(pruned)), nil
}

// MarshalJSON trims default values before emitting JSON (used by YAML library).
func (c Cluster) MarshalJSON() ([]byte, error) {
	pruned := pruneClusterDefaults(c)
	out := structToMap(reflect.ValueOf(pruned))

	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cluster to JSON: %w", err)
	}

	return b, nil
}

// structToMap converts a struct to a map[string]any using reflection.
// It respects json tags for field names and omits zero/empty values.
//
//nolint:cyclop // reflection-based conversion requires checking multiple conditions
func structToMap(val reflect.Value) map[string]any {
	result := make(map[string]any)

	// Handle pointers
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil
		}

		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil
	}

	structType := val.Type()

	for fieldIndex := range structType.NumField() {
		field := structType.Field(fieldIndex)
		fieldValue := val.Field(fieldIndex)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded/inline fields (like metav1.TypeMeta with json:",inline")
		jsonTag := field.Tag.Get("json")
		if strings.Contains(jsonTag, ",inline") || strings.Contains(jsonTag, ",squash") {
			// Merge inline struct fields into parent
			if nested := structToMap(fieldValue); nested != nil {
				maps.Copy(result, nested)
			}

			continue
		}

		// Get JSON field name
		jsonName := getJSONFieldName(field)
		if jsonName == "" || jsonName == "-" {
			continue
		}

		// Convert the field value
		converted := convertValue(fieldValue)
		if converted == nil {
			continue
		}

		// Check if it's an empty map/slice - omit if so
		if m, ok := converted.(map[string]any); ok && len(m) == 0 {
			continue
		}

		result[jsonName] = converted
	}

	return result
}

// getJSONFieldName extracts the JSON field name from struct tags.
func getJSONFieldName(field reflect.StructField) string {
	jsonTag := field.Tag.Get("json")
	if jsonTag == "" {
		return field.Name
	}

	parts := strings.Split(jsonTag, ",")

	return parts[0]
}

// convertValue converts a reflect.Value to a serializable value.
// Returns nil for zero/empty values that should be omitted.
func convertValue(val reflect.Value) any {
	// Handle pointers first
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil
		}

		val = val.Elem()
	}

	// Handle interface values
	if val.Kind() == reflect.Interface {
		if val.IsNil() {
			return nil
		}

		val = val.Elem()
	}

	return convertByKind(val)
}

// convertByKind handles the actual type conversion.
//

func convertByKind(val reflect.Value) any {
	switch val.Kind() {
	case reflect.String:
		return convertString(val)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return convertInt(val)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return convertUint(val)

	case reflect.Bool:
		return convertBool(val)

	case reflect.Slice, reflect.Array:
		return convertSlice(val)

	case reflect.Map:
		return convertMap(val)

	case reflect.Struct:
		return handleStructValue(val)

	default:
		return nil
	}
}

func convertString(val reflect.Value) any {
	str := val.String()
	if str == "" {
		return nil
	}

	return str
}

func convertInt(val reflect.Value) any {
	intVal := val.Int()
	if intVal == 0 {
		return nil
	}

	// Special handling for time.Duration (used in metav1.Duration)
	if val.Type() == reflect.TypeFor[time.Duration]() {
		return val.Interface().(time.Duration).String() //nolint:forcetypeassert // known type
	}

	return intVal
}

func convertUint(val reflect.Value) any {
	uintVal := val.Uint()
	if uintVal == 0 {
		return nil
	}

	return uintVal
}

func convertBool(val reflect.Value) any {
	boolVal := val.Bool()
	if !boolVal {
		return nil
	}

	return boolVal
}

func convertSlice(val reflect.Value) any {
	if val.Len() == 0 {
		return nil
	}

	return val.Interface()
}

func convertMap(val reflect.Value) any {
	if val.Len() == 0 {
		return nil
	}

	return val.Interface()
}

// handleStructValue handles struct type conversion.
func handleStructValue(val reflect.Value) any {
	// Special handling for metav1.Duration
	if val.Type() == reflect.TypeFor[metav1.Duration]() {
		dur := val.Interface().(metav1.Duration) //nolint:forcetypeassert // known type
		if dur.Duration == 0 {
			return nil
		}

		return dur.Duration.String()
	}

	// For other structs, recursively convert
	mapped := structToMap(val)
	if len(mapped) == 0 {
		return nil
	}

	return mapped
}

// pruneClusterDefaults zeroes fields that match default values so they are omitted when marshalled.
func pruneClusterDefaults(cluster Cluster) Cluster {
	// Distribution defaults - needed for context derivation
	distribution := cluster.Spec.Cluster.Distribution
	if distribution == "" {
		distribution = DistributionVanilla
	}

	// Apply context-dependent pruning rules first
	for _, rule := range contextDependentPruneRules(distribution) {
		rule(&cluster)
	}

	// Then apply automatic type-based pruning using reflection
	pruneDefaults(reflect.ValueOf(&cluster).Elem())

	return cluster
}

// pruneRule is a function that modifies a cluster to prune default values.
type pruneRule func(*Cluster)

// contextDependentPruneRules returns pruning rules that depend on runtime context.
// These cannot be handled automatically because the default depends on other field values.
func contextDependentPruneRules(distribution Distribution) []pruneRule {
	return []pruneRule{
		// DistributionConfig default depends on the distribution type
		func(c *Cluster) {
			expected := ExpectedDistributionConfigName(distribution)

			trimmed := strings.TrimSpace(c.Spec.Cluster.DistributionConfig)
			if trimmed == "" || trimmed == expected {
				c.Spec.Cluster.DistributionConfig = ""
			}
		},
		// Connection.Context default depends on the distribution type
		func(c *Cluster) {
			if defaultCtx := ExpectedContextName(
				distribution,
			); c.Spec.Cluster.Connection.Context == defaultCtx {
				c.Spec.Cluster.Connection.Context = ""
			}
		},
	}
}

// Defaulter is implemented by types that have a non-zero default value.
// When marshalling, fields matching their default will be omitted.
// To add a new type with a default, just implement this interface on the type.
type Defaulter interface {
	Default() any
}

// defaulterType is the reflect.Type for the Defaulter interface.
//
//nolint:gochecknoglobals // cached for performance
var defaulterType = reflect.TypeFor[Defaulter]()

// pruneDefaults recursively prunes default values from a struct using reflection.
func pruneDefaults(val reflect.Value) {
	pruneDefaultsWithPath(val, "")
}

// pruneDefaultsWithPath recursively prunes default values, tracking the field path.
func pruneDefaultsWithPath(val reflect.Value, path string) {
	// Handle pointers
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return
		}

		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return
	}

	structType := val.Type()

	for fieldIdx := range structType.NumField() {
		field := structType.Field(fieldIdx)
		fieldVal := val.Field(fieldIdx)

		if !field.IsExported() || !fieldVal.CanSet() {
			continue
		}

		// Build the field path for lookups
		fieldPath := field.Name
		if path != "" {
			fieldPath = path + "." + field.Name
		}

		pruneField(fieldVal, fieldPath, field)
	}
}

// pruneField prunes a single field based on its type, struct tag, or path.
func pruneField(fieldVal reflect.Value, fieldPath string, field reflect.StructField) {
	fieldType := fieldVal.Type()

	// Check if this type implements Defaulter interface
	// We need to check pointer-to-type since methods are on pointer receivers
	ptrType := reflect.PointerTo(fieldType)
	if ptrType.Implements(defaulterType) {
		// Create a pointer to the field value to call the method
		ptr := reflect.New(fieldType)
		ptr.Elem().Set(fieldVal)

		// Call Default() method
		defaultMethod := ptr.MethodByName("Default")
		results := defaultMethod.Call(nil)
		defaultVal := results[0].Interface()

		// Compare current value with default
		if fieldVal.Interface() == defaultVal {
			fieldVal.Set(reflect.Zero(fieldType))
		}

		return
	}

	// Check for default tag on the field
	if defaultTag := field.Tag.Get("default"); defaultTag != "" {
		if pruneByDefaultTag(fieldVal, defaultTag) {
			return
		}
	}

	// Recurse into nested structs
	if fieldVal.Kind() == reflect.Struct {
		pruneDefaultsWithPath(fieldVal, fieldPath)
	}
}

// pruneByDefaultTag prunes a field if it matches the default value specified in the struct tag.
// Returns true if the field was handled (regardless of whether it was pruned).
//

func pruneByDefaultTag(fieldVal reflect.Value, defaultTag string) bool {
	switch fieldVal.Kind() {
	case reflect.String:
		pruneStringDefault(fieldVal, defaultTag)

		return true

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		pruneIntDefault(fieldVal, defaultTag)

		return true

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		pruneUintDefault(fieldVal, defaultTag)

		return true

	case reflect.Bool:
		pruneBoolDefault(fieldVal, defaultTag)

		return true

	default:
		return false
	}
}

func pruneStringDefault(fieldVal reflect.Value, defaultTag string) {
	if fieldVal.String() == defaultTag {
		fieldVal.SetString("")
	}
}

func pruneIntDefault(fieldVal reflect.Value, defaultTag string) {
	var defaultInt int64

	_, err := fmt.Sscanf(defaultTag, "%d", &defaultInt)
	if err != nil {
		return
	}

	if fieldVal.Int() == defaultInt {
		fieldVal.SetInt(0)
	}
}

func pruneUintDefault(fieldVal reflect.Value, defaultTag string) {
	var defaultUint uint64

	_, err := fmt.Sscanf(defaultTag, "%d", &defaultUint)
	if err != nil {
		return
	}

	if fieldVal.Uint() == defaultUint {
		fieldVal.SetUint(0)
	}
}

func pruneBoolDefault(fieldVal reflect.Value, defaultTag string) {
	defaultBool := strings.EqualFold(defaultTag, "true")
	if fieldVal.Bool() == defaultBool {
		fieldVal.SetBool(false)
	}
}

package configmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	mapstructure "github.com/go-viper/mapstructure/v2"
	"gopkg.in/yaml.v3"
)

// declarativeConfigReference is the documentation page listing every valid
// ksail.yaml key, named by the unknown-key warning when no close match exists.
const declarativeConfigReference = "https://ksail.devantler.tech/configuration/declarative-configuration/"

// UnknownKey is a key in a ksail config file that KSail does not read: the
// config decoder ignores it, so a misspelled or misplaced key would otherwise
// read as a successful load while its setting silently stays at the default
// (issue #6980).
type UnknownKey struct {
	// Path is the dotted path to the key as written in the file (original
	// casing, list elements as [i]), e.g. spec.cluster.eks.experimentalControlPlaneUpgradeTYPO.
	Path string
	// Suggestion is the closest valid key at the same level, or "" when none is
	// close enough to be a likely misspelling.
	Suggestion string
}

// FindUnknownKeys reports the keys in a ksail config file's content that the
// config loader ignores. It decodes the content with the same mapstructure
// rules the loader uses (field matching, squashed embeds, decode hooks), so a
// key is reported exactly when loading would drop it — never a key the loader
// reads, and never a flag or environment binding, which are not part of the
// file. The result is sorted by path. Content that is not a YAML mapping yields
// no keys: reading it fails the load with its own error.
func FindUnknownKeys(content []byte) []UnknownKey {
	var raw map[string]any

	err := yaml.Unmarshal(content, &raw)
	if err != nil || raw == nil {
		return nil
	}

	var metadata mapstructure.Metadata

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		// Mirror viper's decoder defaults plus the loader's own hooks, so the
		// unused-key set matches what the real load ignores.
		WeaklyTypedInput: true,
		DecodeHook:       clusterDecodeHook,
		Metadata:         &metadata,
		Result:           v1alpha1.NewCluster(),
	})
	if err != nil {
		return nil
	}

	// A value of the wrong type is the real load's error to report; the
	// unused-key metadata is still collected for every other key.
	_ = decoder.Decode(raw)

	unknown := make([]UnknownKey, 0, len(metadata.Unused))
	for _, keyPath := range metadata.Unused {
		keyPath = originalKeyPath(raw, keyPath)

		unknown = append(unknown, UnknownKey{
			Path:       keyPath,
			Suggestion: suggestKey(keyPath),
		})
	}

	slices.SortFunc(unknown, func(left, right UnknownKey) int {
		return strings.Compare(left.Path, right.Path)
	})

	return unknown
}

// warnUnknownConfigKeys writes one warning per key in the loaded config file
// that the loader ignores, naming the key's path and the fix in the same
// field:/fix: shape as validation errors. It is a warning, not an error: configs
// carrying stray keys load today, and rejecting them would break those
// workspaces on upgrade, while the warning already removes the silent part of
// the failure. A file that cannot be re-read is skipped; the load reports it.
func (m *ConfigManager) warnUnknownConfigKeys() {
	configFile := m.Viper.ConfigFileUsed()
	if configFile == "" {
		return
	}

	//nolint:gosec // G304: the path is the config file viper has just read.
	content, err := os.ReadFile(configFile)
	if err != nil {
		return
	}

	for _, key := range FindUnknownKeys(content) {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "unknown key in %s is ignored\nfield: %s\nfix: %s",
			Args:    []any{filepath.Base(configFile), key.Path, unknownKeyFix(key)},
			Writer:  m.Writer,
		})
	}
}

// unknownKeyFix names the fix for an unknown key: the likely intended key when
// one is close, otherwise removing it or checking it against the reference.
func unknownKeyFix(key UnknownKey) string {
	if key.Suggestion != "" {
		return fmt.Sprintf("did you mean '%s'? Rename the key, or remove it", key.Suggestion)
	}

	return "remove the key, or check its spelling and nesting against " + declarativeConfigReference
}

// listIndexPattern matches the [i] list-element suffixes mapstructure appends
// to a path segment.
var listIndexPattern = regexp.MustCompile(`\[(\d+)\]`)

// originalKeyPath rewrites a decoder key path — whose parent segments are the
// Go field names the decoder matched, e.g. spec.Cluster.EKS.x — into the keys as
// written in the file (spec.cluster.eks.x), by resolving each segment
// case-insensitively against the raw content. A path that cannot be resolved is
// returned unchanged.
func originalKeyPath(raw map[string]any, keyPath string) string {
	var node any = raw

	segments := strings.Split(keyPath, ".")
	resolved := make([]string, 0, len(segments))

	for _, segment := range segments {
		name := listIndexPattern.ReplaceAllString(segment, "")

		mapping, isMapping := node.(map[string]any)
		if !isMapping {
			return keyPath
		}

		key, found := lookupKeyFold(mapping, name)
		if !found {
			return keyPath
		}

		node = mapping[key]

		for _, match := range listIndexPattern.FindAllStringSubmatch(segment, -1) {
			list, isList := node.([]any)

			index, err := strconv.Atoi(match[1])
			if !isList || err != nil || index >= len(list) {
				return keyPath
			}

			node = list[index]
		}

		resolved = append(resolved, key+segment[len(name):])
	}

	return strings.Join(resolved, ".")
}

// lookupKeyFold finds the key of mapping equal to name, preferring an exact
// match over a case-insensitive one (the decoder matches keys either way).
func lookupKeyFold(mapping map[string]any, name string) (string, bool) {
	if _, ok := mapping[name]; ok {
		return name, true
	}

	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for _, key := range keys {
		if strings.EqualFold(key, name) {
			return key, true
		}
	}

	return "", false
}

// suggestKey returns the valid key closest to the last segment of keyPath among
// the keys accepted at its parent, or "" when the parent is not a struct (a map
// or free-form value accepts any key) or no candidate is close enough to be a
// likely misspelling.
func suggestKey(keyPath string) string {
	segments := strings.Split(listIndexPattern.ReplaceAllString(keyPath, ""), ".")
	parent := reflect.TypeFor[v1alpha1.Cluster]()

	for _, segment := range segments[:len(segments)-1] {
		field, ok := findField(parent, segment)
		if !ok {
			return ""
		}

		parent = elemType(field.Type)
	}

	if parent.Kind() != reflect.Struct {
		return ""
	}

	return closestKey(segments[len(segments)-1], structKeys(parent))
}

// findField finds the struct field of structType that the decoder maps key to,
// following the same rules: the mapstructure tag name (else the Go field name),
// matched case-insensitively, with squashed embeds flattened.
func findField(structType reflect.Type, key string) (reflect.StructField, bool) {
	if structType.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}

	for field := range structFields(structType) {
		if strings.EqualFold(decoderName(field), key) {
			return field, true
		}
	}

	return reflect.StructField{}, false
}

// structKeys lists the documented keys a struct accepts, in the spelling the
// docs and schema use (the json tag name, falling back to the decoder name).
// Fields hidden from the schema (json "-") are internal and never suggested.
func structKeys(structType reflect.Type) []string {
	keys := []string{}

	for field := range structFields(structType) {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}

		if name == "" {
			name = decoderName(field)
		}

		keys = append(keys, name)
	}

	return keys
}

// structFields yields the exported, decodable fields of structType, descending
// into squashed (mapstructure ",squash") embedded structs.
func structFields(structType reflect.Type) func(yield func(reflect.StructField) bool) {
	return func(yield func(reflect.StructField) bool) {
		for field := range structType.Fields() {
			tag := field.Tag.Get("mapstructure")
			if !field.IsExported() || tag == "-" {
				continue
			}

			if strings.Contains(tag, ",squash") {
				for embedded := range structFields(elemType(field.Type)) {
					if !yield(embedded) {
						return
					}
				}

				continue
			}

			if !yield(field) {
				return
			}
		}
	}
}

// decoderName is the key the decoder matches a field against: its mapstructure
// tag name, or its Go field name when untagged.
func decoderName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("mapstructure"), ",")
	if name == "" {
		return field.Name
	}

	return name
}

// containerKinds are the kinds whose element type, not their own, is what the
// decoder maps a config value's keys onto.
//
//nolint:gochecknoglobals // read-only lookup table
var containerKinds = []reflect.Kind{reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map}

// elemType unwraps pointers, slices, arrays and maps to the type their
// elements are decoded into.
func elemType(fieldType reflect.Type) reflect.Type {
	for slices.Contains(containerKinds, fieldType.Kind()) {
		fieldType = fieldType.Elem()
	}

	return fieldType
}

// closestKey returns the candidate with the smallest case-insensitive edit
// distance to key, provided the distance is small enough — at most a third of
// the candidate's length, and never more than a few edits for short keys — to
// read as a misspelling rather than an unrelated key.
func closestKey(key string, candidates []string) string {
	const minTolerance = 2

	best, bestDistance := "", -1

	for _, candidate := range candidates {
		distance := editDistance(strings.ToLower(key), strings.ToLower(candidate))

		tolerance := max(minTolerance, len(candidate)/3) //nolint:mnd // a third of the key's length
		if distance > tolerance {
			continue
		}

		if bestDistance == -1 || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}

	return best
}

// editDistance is the Levenshtein distance between two strings, in runes.
func editDistance(left, right string) int {
	leftRunes, rightRunes := []rune(left), []rune(right)
	previous := make([]int, len(rightRunes)+1)
	current := make([]int, len(rightRunes)+1)

	for col := range previous {
		previous[col] = col
	}

	for row := 1; row <= len(leftRunes); row++ {
		current[0] = row

		for col := 1; col <= len(rightRunes); col++ {
			cost := 1
			if leftRunes[row-1] == rightRunes[col-1] {
				cost = 0
			}

			current[col] = min(previous[col]+1, current[col-1]+1, previous[col-1]+cost)
		}

		previous, current = current, previous
	}

	return previous[len(rightRunes)]
}

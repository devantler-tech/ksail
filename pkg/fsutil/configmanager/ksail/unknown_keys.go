package configmanager

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	mapstructure "github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
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
// config loader ignores. It reads the content through viper and decodes the
// result with the same mapstructure rules the loader uses (field matching,
// squashed embeds, decode hooks), so a key is reported exactly when loading
// would drop it — never a key the loader reads (including a dotted key viper
// splits into its nested path), and never a flag or environment binding, which
// are not part of the file. The result is sorted by path. Content that is not a
// YAML mapping yields no keys: reading it fails the load with its own error.
func FindUnknownKeys(content []byte) []UnknownKey {
	var raw map[string]any

	err := yaml.Unmarshal(content, &raw)
	if err != nil || raw == nil {
		return nil
	}

	settings, err := loaderSettings(content)
	if err != nil {
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
	_ = decoder.Decode(settings)

	aliased := aliasedPaths(content)

	unknown := make([]UnknownKey, 0, len(metadata.Unused))
	for _, keyPath := range metadata.Unused {
		suggestionPath, paths := originalKeyPath(raw, keyPath)
		suggestion := suggestKey(suggestionPath)

		for _, path := range paths {
			if usedByAlias(aliased, path) {
				continue
			}

			unknown = append(unknown, UnknownKey{Path: path, Suggestion: suggestion})
		}
	}

	slices.SortFunc(unknown, func(left, right UnknownKey) int {
		return strings.Compare(left.Path, right.Path)
	})

	return unknown
}

// loaderSettings reads content the way the loader does, through viper, so a
// dotted key is split into its nested path and every key is a string.
func loaderSettings(content []byte) (map[string]any, error) {
	fileViper := viper.New()
	fileViper.SetConfigType("yaml")

	err := fileViper.ReadConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("reading config content: %w", err)
	}

	return fileViper.AllSettings(), nil
}

// aliasedPaths lists the paths, in the reported format, of the values in the
// content's first document that a YAML alias refers to. A key holding such a
// value is used by the alias (for example merged with <<: *anchor), so it is
// not reported even though the decoder never reads it by name.
func aliasedPaths(content []byte) []string {
	var document yaml.Node

	err := yaml.Unmarshal(content, &document)
	if err != nil {
		return nil
	}

	targets := map[*yaml.Node]bool{}
	collectAliasTargets(&document, targets)

	if len(targets) == 0 {
		return nil
	}

	paths := []string{}
	collectAliasedPaths(&document, "", targets, &paths)

	return paths
}

// usedByAlias reports whether the value at path, or a value under it, is one
// an alias refers to.
func usedByAlias(aliased []string, path string) bool {
	return slices.ContainsFunc(aliased, func(aliasedPath string) bool {
		return aliasedPath == path ||
			strings.HasPrefix(aliasedPath, path+".") ||
			strings.HasPrefix(aliasedPath, path+"[")
	})
}

// collectAliasTargets records the node each alias under node refers to.
func collectAliasTargets(node *yaml.Node, targets map[*yaml.Node]bool) {
	if node.Kind == yaml.AliasNode {
		if node.Alias != nil {
			targets[node.Alias] = true
		}

		return
	}

	for _, child := range node.Content {
		collectAliasTargets(child, targets)
	}
}

// collectAliasedPaths appends the path of every value under node that is an
// alias target, naming mapping keys as written and list elements as [i].
func collectAliasedPaths(
	node *yaml.Node,
	path string,
	targets map[*yaml.Node]bool,
	paths *[]string,
) {
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			collectAliasedPaths(child, path, targets, paths)
		}
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			childPath := node.Content[index].Value
			if path != "" {
				childPath = path + "." + childPath
			}

			collectAliasedValue(node.Content[index+1], childPath, targets, paths)
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			collectAliasedValue(child, fmt.Sprintf("%s[%d]", path, index), targets, paths)
		}
	case yaml.ScalarNode, yaml.AliasNode:
	}
}

// collectAliasedValue records value's path when it is an alias target, then
// descends into it.
func collectAliasedValue(
	value *yaml.Node,
	path string,
	targets map[*yaml.Node]bool,
	paths *[]string,
) {
	if targets[value] {
		*paths = append(*paths, path)
	}

	collectAliasedPaths(value, path, targets, paths)
}

// hasIgnoredYAMLDocuments reports whether content holds a YAML document with
// content after the first one. The loader reads only the first document, so
// everything in the others is silently ignored. A bare document marker, as in
// a trailing ---, holds nothing and does not count.
func hasIgnoredYAMLDocuments(content []byte) bool {
	decoder := yaml.NewDecoder(bytes.NewReader(content))

	for index := 0; ; index++ {
		var document yaml.Node

		err := decoder.Decode(&document)
		if err != nil {
			return false
		}

		if index > 0 && !isEmptyDocument(&document) {
			return true
		}
	}
}

// isEmptyDocument reports whether a decoded document holds nothing but null.
func isEmptyDocument(document *yaml.Node) bool {
	for _, child := range document.Content {
		if child.Kind != yaml.ScalarNode || child.ShortTag() != "!!null" {
			return false
		}
	}

	return true
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

	if hasIgnoredYAMLDocuments(content) {
		notify.WriteMessage(notify.Message{
			Type: notify.WarningType,
			Content: "only the first YAML document in %s is read\n" +
				"fix: move the settings of the other documents into the first one, or remove them",
			Args:   []any{filepath.Base(configFile)},
			Writer: m.Writer,
		})
	}

	for _, key := range FindUnknownKeys(content) {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "unknown key in %s is ignored\nfield: %s\nfix: %s",
			Args:    []any{filepath.Base(configFile), displayKeyPath(key.Path), unknownKeyFix(key)},
			Writer:  m.Writer,
		})
	}
}

// displayKeyPath escapes the non-printable characters of a key path for the
// warning, so a key name in the config cannot send control sequences to the
// terminal.
func displayKeyPath(path string) string {
	var display strings.Builder

	for _, char := range path {
		if unicode.IsPrint(char) {
			display.WriteRune(char)

			continue
		}

		quoted := strconv.QuoteRune(char)
		display.WriteString(quoted[1 : len(quoted)-1])
	}

	return display.String()
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
// written in the file (spec.cluster.eks.x), by resolving its segments
// case-insensitively against the raw content, where one dotted key may spell
// several of them. It returns the path to suggest a fix for and the written
// paths to report: usually one and the same, but an unknown key that is only
// the leading part of dotted keys (totally in totally.bogus) is reported as each
// of those keys. A path that cannot be resolved is returned unchanged.
func originalKeyPath(raw map[string]any, keyPath string) (string, []string) {
	var node any = raw

	segments := strings.Split(keyPath, ".")
	resolved := make([]string, 0, len(segments))
	levels := []keyLevel{}

	for len(segments) > 0 {
		mapping, isMapping := stringKeyed(node)
		if !isMapping {
			return keyPath, []string{keyPath}
		}

		levels = append(
			levels,
			keyLevel{mapping: mapping, resolved: slices.Clone(resolved), rest: segments},
		)

		key, used, found := lookupDottedKeyFold(mapping, segments)
		if !found {
			return dottedKeyPaths(keyPath, levels)
		}

		segment := segments[used-1]
		name := listIndexPattern.ReplaceAllString(segment, "")
		segments = segments[used:]
		node = mapping[key]

		for _, match := range listIndexPattern.FindAllStringSubmatch(segment, -1) {
			list, isList := node.([]any)

			index, err := strconv.Atoi(match[1])
			if !isList || err != nil || index >= len(list) {
				return keyPath, []string{keyPath}
			}

			node = list[index]
		}

		resolved = append(resolved, key+segment[len(name):])
	}

	path := strings.Join(resolved, ".")

	return path, []string{path}
}

// keyLevel is a mapping visited while resolving a decoder path, with the path
// resolved up to it and the decoder segments that remained there.
type keyLevel struct {
	mapping  map[string]any
	resolved []string
	rest     []string
}

// dottedKeyPaths resolves a decoder path whose remaining segments no key
// spells, as the leading part of dotted keys: viper splits totally.bogus into
// totally → bogus, and merges a root spec.clustr.distribution into the spec
// mapping, while the decoder reports only the unknown totally or spec.clustr.
// Each such key is reported in full, as written, in whichever visited mapping
// holds it. When there is none, the decoder path is returned unchanged.
func dottedKeyPaths(keyPath string, levels []keyLevel) (string, []string) {
	paths := make([]string, 0, len(levels))

	for _, level := range levels {
		paths = append(paths, dottedKeysAt(level)...)
	}

	if len(paths) == 0 {
		return keyPath, []string{keyPath}
	}

	slices.Sort(paths)

	deepest := levels[len(levels)-1]

	return strings.Join(slices.Concat(deepest.resolved, deepest.rest), "."), paths
}

// dottedKeysAt lists the full paths of the keys of level's mapping that its
// remaining segments are the leading part of.
func dottedKeysAt(level keyLevel) []string {
	if slices.ContainsFunc(level.rest, listIndexPattern.MatchString) {
		return nil
	}

	prefix := strings.ToLower(strings.Join(level.rest, ".")) + "."

	paths := []string{}

	for key := range level.mapping {
		if strings.HasPrefix(strings.ToLower(key), prefix) {
			paths = append(paths, strings.Join(slices.Concat(level.resolved, []string{key}), "."))
		}
	}

	return paths
}

// stringKeyed returns node as a string-keyed mapping, converting the
// map[any]any YAML produces for a mapping with a non-string key.
func stringKeyed(node any) (map[string]any, bool) {
	switch mapping := node.(type) {
	case map[string]any:
		return mapping, true
	case map[any]any:
		converted := make(map[string]any, len(mapping))
		for key, value := range mapping {
			converted[fmt.Sprint(key)] = value
		}

		return converted, true
	default:
		return nil, false
	}
}

// lookupDottedKeyFold finds the key of mapping that spells the longest leading
// run of segments joined with dots, because viper splits a dotted key such as
// cluster.connection.context into its nested path. Only the run's last segment
// may carry a list index. It returns the key and how many segments it spells.
func lookupDottedKeyFold(mapping map[string]any, segments []string) (string, int, bool) {
	for used := len(segments); used > 0; used-- {
		if slices.ContainsFunc(segments[:used-1], listIndexPattern.MatchString) {
			continue
		}

		name := listIndexPattern.ReplaceAllString(strings.Join(segments[:used], "."), "")

		key, found := lookupKeyFold(mapping, name)
		if found {
			return key, used, true
		}
	}

	return "", 0, false
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

package v1alpha1_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// jsonschemaTagOptionKeys returns the struct-tag option keys the
// invopop/jsonschema reflector understands. Everything the tag parser splits
// off that is not one of these is almost certainly prose severed from a
// description by a raw comma — the tag options are comma-separated, so a comma
// inside a description= value silently truncates it in the published schema
// and the generated configuration reference (#6035).
func jsonschemaTagOptionKeys() map[string]bool {
	return map[string]bool{
		"anyof_ref":        true,
		"anyof_required":   true,
		"anyof_type":       true,
		"const":            true,
		"default":          true,
		"deprecated":       true,
		"description":      true,
		"enum":             true,
		"example":          true,
		"exclusiveMaximum": true,
		"exclusiveMinimum": true,
		"format":           true,
		"maxItems":         true,
		"maxLength":        true,
		"maxProperties":    true,
		"maximum":          true,
		"minItems":         true,
		"minLength":        true,
		"minProperties":    true,
		"minimum":          true,
		"multipleOf":       true,
		"nullable":         true,
		"oneof_ref":        true,
		"oneof_required":   true,
		"oneof_type":       true,
		"pattern":          true,
		"readOnly":         true,
		"required":         true,
		"title":            true,
		"uniqueItems":      true,
		"writeOnly":        true,
		"-":                true,
	}
}

// TestJSONSchemaTagsContainNoSeveredDescriptions pins the whole failure class
// behind #6035: a raw comma inside any jsonschema tag value splits off a
// fragment the reflector treats as an unknown option and drops, truncating the
// text users see in IDE schema hovers and the docs reference. It parses this
// package's sources and fails on any tag piece that is not a recognised
// option, naming the file, line, and fragment.
func TestJSONSchemaTagsContainNoSeveredDescriptions(t *testing.T) {
	t.Parallel()

	knownOptionKeys := jsonschemaTagOptionKeys()
	fileSet := token.NewFileSet()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("listing package sources: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}

		file, err := parser.ParseFile(fileSet, entry.Name(), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}

		ast.Inspect(file, func(node ast.Node) bool {
			field, isField := node.(*ast.Field)
			if !isField || field.Tag == nil {
				return true
			}

			assertNoSeveredTagPieces(t, fileSet, field, knownOptionKeys)

			return true
		})
	}
}

// assertNoSeveredTagPieces splits one field's jsonschema tag the way the
// reflector does and reports any piece that is not a known option key.
func assertNoSeveredTagPieces(
	t *testing.T,
	fileSet *token.FileSet,
	field *ast.Field,
	knownOptionKeys map[string]bool,
) {
	t.Helper()

	rawTag, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return
	}

	schemaTag, hasTag := reflect.StructTag(rawTag).Lookup("jsonschema")
	if !hasTag {
		return
	}

	for piece := range strings.SplitSeq(schemaTag, ",") {
		key, _, _ := strings.Cut(piece, "=")
		if knownOptionKeys[key] {
			continue
		}

		t.Errorf(
			"%s: jsonschema tag piece %q is not a known option — a raw comma has severed"+
				" it from the preceding value, silently truncating the published schema;"+
				" reword the value without commas",
			fileSet.Position(field.Tag.Pos()), piece,
		)
	}
}

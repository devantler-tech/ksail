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

// TestGeneratedCRDCarriesCurrentDescriptions pins the generated operator CRD
// to the doc comments it is rendered from: the schema-focused test above cannot
// see charts/ksail-operator/crds/ksail.io_clusters.yaml (controller-gen renders
// CRD descriptions from doc comments, not jsonschema tags), so a doc-comment
// fix that ships without `make generate` would leave the chart's CRD stale and
// still pass. YAML wraps long descriptions across indented lines, so the
// comparison collapses all whitespace before matching.
func TestGeneratedCRDCarriesCurrentDescriptions(t *testing.T) {
	t.Parallel()

	crdPath := filepath.Join(
		"..", "..", "..", "..",
		"charts", "ksail-operator", "crds", "ksail.io_clusters.yaml",
	)

	raw, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("reading generated CRD: %v", err)
	}

	normalized := strings.Join(strings.Fields(string(raw)), " ")

	for _, want := range []string{
		// ClusterSpec.DistributionConfig doc comment (types.go).
		"(e.g. kind.yaml, k3d.yaml, vcluster.yaml, kwok.yaml, eks.yaml, gke.yaml," +
			" aks.yaml, or the talos directory).",
		"When empty, KSail uses the distribution's default configuration path.",
	} {
		if !strings.Contains(normalized, want) {
			t.Errorf(
				"generated CRD %s is stale: missing doc-comment text %q —"+
					" run `make generate` after editing doc comments",
				crdPath, want,
			)
		}
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
		if key == "description" {
			t.Errorf(
				"%s: jsonschema tag carries an inline description= — the tag's options are"+
					" comma-separated, so any comma in the prose silently truncates the"+
					" published schema (#6035); move the text to a jsonschema_description tag,"+
					" which has no option grammar and keeps commas intact",
				fileSet.Position(field.Tag.Pos()),
			)

			continue
		}

		if knownOptionKeys[key] {
			continue
		}

		t.Errorf(
			"%s: jsonschema tag piece %q is not a known option — a raw comma has severed"+
				" it from the preceding value, silently truncating the published schema;"+
				" move prose to jsonschema_description or reword the value without commas",
			fileSet.Position(field.Tag.Pos()), piece,
		)
	}
}

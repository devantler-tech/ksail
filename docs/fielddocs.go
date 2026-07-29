// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

package docs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// FieldDocs maps a struct type name to its field documentation extracted from
// Go doc comments. The empty field-name key ("") holds the type-level doc
// comment of the struct itself.
type FieldDocs map[string]map[string]string

// LoadFieldDocs parses the non-test Go source files in dir and extracts the
// doc comments of every struct type and its named fields, collapsed to single
// MDX-safe lines for use in generated Markdown tables.
func LoadFieldDocs(dir string) (FieldDocs, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	fileSet := token.NewFileSet()
	fieldDocs := FieldDocs{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(
			fileSet, filepath.Join(dir, name), nil, parser.ParseComments,
		)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}

		collectFileFieldDocs(file, fieldDocs)
	}

	return fieldDocs, nil
}

// collectFileFieldDocs walks a parsed file's type declarations and records
// struct and field doc comments into fieldDocs.
func collectFileFieldDocs(file *ast.File, fieldDocs FieldDocs) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
			if !isTypeSpec {
				continue
			}

			structType, isStruct := typeSpec.Type.(*ast.StructType)
			if !isStruct {
				continue
			}

			fieldDocs[typeSpec.Name.Name] = collectStructFieldDocs(genDecl, typeSpec, structType)
		}
	}
}

// collectStructFieldDocs extracts the type-level doc comment (stored under the
// "" key) and per-field doc comments for a single struct declaration.
func collectStructFieldDocs(
	genDecl *ast.GenDecl,
	typeSpec *ast.TypeSpec,
	structType *ast.StructType,
) map[string]string {
	fields := map[string]string{}

	typeDoc := typeSpec.Doc
	if typeDoc == nil {
		typeDoc = genDecl.Doc
	}

	if typeDoc != nil {
		fields[""] = mdxTableCell(typeDoc.Text())
	}

	for _, field := range structType.Fields.List {
		if field.Doc == nil {
			continue
		}

		doc := mdxTableCell(field.Doc.Text())

		for _, name := range field.Names {
			fields[name.Name] = doc
		}
	}

	return fields
}

// mdxTableCell collapses text to a single line and escapes characters that
// would break MDX rendering or Markdown table layout (JSX/expression
// delimiters and the table cell separator).
func mdxTableCell(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	replacer := strings.NewReplacer(
		"<", "&lt;",
		">", "&gt;",
		"{", "&#123;",
		"}", "&#125;",
		"|", "\\|",
	)

	return replacer.Replace(collapsed)
}

// describe resolves the documentation for a struct field: explicit jsonschema
// struct tag descriptions win, then the field's own doc comment, then (for
// struct-typed fields) the doc comment of the field's type.
func (d FieldDocs) describe(owner reflect.Type, field reflect.StructField) string {
	if desc := tagDescription(field); desc != "" {
		return mdxTableCell(desc)
	}

	if doc := d[owner.Name()][field.Name]; doc != "" {
		return doc
	}

	return d.typeDoc(field.Type)
}

// typeDoc returns the type-level doc comment for struct-typed fields
// (unwrapping pointers and slices); empty for non-struct types.
func (d FieldDocs) typeDoc(fieldType reflect.Type) string {
	elem := unwrapType(fieldType)
	if elem.Kind() != reflect.Struct {
		return ""
	}

	return d[elem.Name()][""]
}

// unwrapType unwraps pointer and slice types to their element type.
func unwrapType(fieldType reflect.Type) reflect.Type {
	for fieldType.Kind() == reflect.Pointer || fieldType.Kind() == reflect.Slice {
		fieldType = fieldType.Elem()
	}

	return fieldType
}

// tagDescription extracts a description from the jsonschema struct tag,
// falling back to the jsonschema_description tag.
func tagDescription(field reflect.StructField) string {
	if tag := field.Tag.Get("jsonschema"); tag != "" {
		for part := range strings.SplitSeq(tag, ",") {
			if after, ok := strings.CutPrefix(part, "description="); ok {
				return after
			}
		}
	}

	return field.Tag.Get("jsonschema_description")
}

// jsonFieldName returns the effective JSON name for an exported, JSON-tagged
// field, or "" when the field must be skipped (unexported, inline, untagged,
// or explicitly excluded with json:"-").
func jsonFieldName(field reflect.StructField) string {
	if !field.IsExported() {
		return ""
	}

	jsonTag := field.Tag.Get("json")
	if jsonTag == "" || jsonTag == ",inline" {
		return ""
	}

	name := strings.Split(jsonTag, ",")[0]
	if name == "-" {
		return ""
	}

	return name
}

// RenderFieldTable renders a Markdown table for the exported JSON-tagged
// fields of structType, using struct tag metadata (default values, jsonschema
// descriptions) and Go doc comments (via fieldDocs) for descriptions. The
// prefix is prepended to every field name (e.g. "talos.").
func RenderFieldTable(structType reflect.Type, prefix string, fieldDocs FieldDocs) string {
	var builder strings.Builder

	builder.WriteString("| Field | Type | Default | Description |\n")
	builder.WriteString("| ----- | ---- | ------- | ----------- |\n")

	for field := range structType.Fields() {
		name := jsonFieldName(field)
		if name == "" {
			continue
		}

		builder.WriteString(fieldTableRow(structType, field, prefix+name, fieldDocs))
	}

	builder.WriteString("\n")

	return builder.String()
}

// fieldTableRow renders a single field row for RenderFieldTable.
func fieldTableRow(
	owner reflect.Type,
	field reflect.StructField,
	fullName string,
	fieldDocs FieldDocs,
) string {
	defaultVal := "–"
	if tagDefault := field.Tag.Get("default"); tagDefault != "" {
		defaultVal = "`" + tagDefault + "`"
	}

	return fmt.Sprintf(
		"| `%s` | %s | %s | %s |\n",
		fullName,
		mdxTableCell(friendlyTypeName(field.Type)),
		defaultVal,
		fieldDocs.describe(owner, field),
	)
}

// friendlyTypeName returns a human-readable type name for a config field.
//
//nolint:exhaustive // The default case handles all other reflect.Kind values.
func friendlyTypeName(fieldType reflect.Type) string {
	switch fieldType.Kind() {
	case reflect.String:
		if reflect.PointerTo(fieldType).Implements(reflect.TypeFor[v1alpha1.EnumValuer]()) {
			return "enum"
		}

		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int32:
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Pointer:
		return friendlyTypeName(fieldType.Elem())
	case reflect.Slice:
		return sliceTypeName(fieldType)
	case reflect.Map:
		return "map[" + friendlyTypeName(fieldType.Key()) + "]" + friendlyTypeName(fieldType.Elem())
	default:
		return fieldType.Name()
	}
}

// sliceTypeName labels a slice-typed field. A scalar-or-array union (see
// [v1alpha1.ScalarOrList]) accepts either a single element or a list of them, so
// both shapes are documented; every other slice is the array-only "[]<element>".
func sliceTypeName(fieldType reflect.Type) string {
	elem := friendlyTypeName(fieldType.Elem())
	if implementsScalarOrList(fieldType) {
		return elem + " | []" + elem
	}

	return "[]" + elem
}

// implementsScalarOrList reports whether fieldType is a scalar-or-array union —
// a slice type that also accepts a single scalar element. Both the value and
// pointer method sets are checked so a marker method with either receiver is
// recognised.
func implementsScalarOrList(fieldType reflect.Type) bool {
	scalarOrList := reflect.TypeFor[v1alpha1.ScalarOrList]()
	if fieldType.Implements(scalarOrList) {
		return true
	}

	return reflect.PointerTo(fieldType).Implements(scalarOrList)
}

// RenderTypeSections renders an "### <path> (<TypeName>)" reference section
// for structType — type doc comment intro plus field table — followed
// depth-first by equivalent sections for every nested KSail API struct field.
// Slice-typed fields get a "[]" path suffix (e.g. "…node.pools[]").
func RenderTypeSections(path string, structType reflect.Type, fieldDocs FieldDocs) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "### %s (%s)\n\n", path, structType.Name())

	if typeDoc := fieldDocs[structType.Name()][""]; typeDoc != "" {
		builder.WriteString(typeDoc)
		builder.WriteString("\n\n")
	}

	builder.WriteString(RenderFieldTable(structType, "", fieldDocs))

	for field := range structType.Fields() {
		childPath, childType, ok := nestedStructPath(path, field)
		if !ok {
			continue
		}

		builder.WriteString(RenderTypeSections(childPath, childType, fieldDocs))
	}

	return builder.String()
}

// nestedStructPath resolves field to a nested KSail API struct type and its
// dotted documentation path; ok is false for skipped or non-API fields.
func nestedStructPath(path string, field reflect.StructField) (string, reflect.Type, bool) {
	jsonName := jsonFieldName(field)
	if jsonName == "" {
		return "", nil, false
	}

	fieldType := field.Type

	suffix := ""

	if fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}

	if fieldType.Kind() == reflect.Slice {
		fieldType = fieldType.Elem()
		suffix = "[]"
	}

	if fieldType.Kind() != reflect.Struct || fieldType.PkgPath() != apiPkgPath() {
		return "", nil, false
	}

	return path + "." + jsonName + suffix, fieldType, true
}

// apiPkgPath returns the import path of the KSail cluster API package whose
// struct types are documented recursively.
func apiPkgPath() string {
	return reflect.TypeFor[v1alpha1.Spec]().PkgPath()
}

package kubescape_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	otelSDKLogModulePath = "go.opentelemetry.io/otel/sdk/log"
	uptracePackagePath   = "github.com/uptrace/uptrace-go/uptrace"
	kubescapeLoggerPath  = "github.com/kubescape/go-logger"
)

// goListPackage mirrors the Go command's exported JSON field names.
type goListPackage struct {
	ImportPath     string   `json:"ImportPath"`     //nolint:tagliatelle // Go command output contract.
	Name           string   `json:"Name"`           //nolint:tagliatelle // Go command output contract.
	Dir            string   `json:"Dir"`            //nolint:tagliatelle // Go command output contract.
	Standard       bool     `json:"Standard"`       //nolint:tagliatelle // Go command output contract.
	GoFiles        []string `json:"GoFiles"`        //nolint:tagliatelle // Go command output contract.
	CgoFiles       []string `json:"CgoFiles"`       //nolint:tagliatelle // Go command output contract.
	IgnoredGoFiles []string `json:"IgnoredGoFiles"` //nolint:tagliatelle // Go command output contract.
	Imports        []string `json:"Imports"`        //nolint:tagliatelle // Go command output contract.
}

// auditedOTelSDKLogVersions pins the go.opentelemetry.io/otel/sdk/log release
// each shipped module links. The advisory GHSA-hjf4-fphr-2h65 (BatchProcessor
// busy-spin, fixed in v0.21.0) affects these versions, and no fix can be
// adopted until uptrace's otelutil compiles against otel/log v0.21 (#7375).
// A move to v0.21.0 or later makes this guard and its risk acceptance
// obsolete: delete both then.
func auditedOTelSDKLogVersions() map[string]string {
	return map[string]string{
		"root":    "v0.19.0",
		"desktop": "v0.19.0",
	}
}

// auditedOTelSDKLogImporters is the complete set of packages allowed to import
// sdk/log. The OTLP log exporter packages only convert and send records; the
// uptrace package is the only one that builds a BatchProcessor, and only from
// ConfigureOpentelemetry.
func auditedOTelSDKLogImporters() map[string]bool {
	return map[string]bool{
		uptracePackagePath: true,
		"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp":                    true,
		"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp/internal/transform": true,
	}
}

// otelBatchEntryPoints names, per package, the exported function through which
// a caller reaches sdklog.NewBatchProcessor: kubescape's logger.InitOtel calls
// uptrace.ConfigureOpentelemetry, which builds one BatchProcessor per DSN.
func otelBatchEntryPoints() map[string]string {
	return map[string]string{
		kubescapeLoggerPath: "InitOtel",
		uptracePackagePath:  "ConfigureOpentelemetry",
	}
}

// allowedEntryPointReferences lists the only places an entry point may be
// referenced, keyed by referencing package, then by entry-point package, and
// valued by the enclosing function. InitOtel is itself an entry point, so the
// reference inside it is covered by the InitOtel check.
func allowedEntryPointReferences() map[string]map[string]string {
	return map[string]map[string]string{
		kubescapeLoggerPath: {uptracePackagePath: "InitOtel"},
	}
}

// TestOTelLogBatchProcessorStaysUnreachable pins the reachability verdict for
// GHSA-hjf4-fphr-2h65 (issue #7375). sdk/log is linked into KSail through
// kubescape's logger and uptrace-go, but its BatchProcessor is only built by
// uptrace.ConfigureOpentelemetry, which only kubescape's logger.InitOtel calls,
// and nothing in KSail's build graph calls InitOtel. The Go vulnerability
// database carries no entry for this advisory, so govulncheck cannot report
// it; this test is the gate instead. It fails when the audited sdk/log release
// changes, when a new package imports sdk/log, or when any package in the
// graph references an entry point outside the allowed places.
//
// Risk accepted 2026-09-30 while #7375's blocker stands: no otelutil/otelzap
// release supports go.opentelemetry.io/otel/log v0.21 or later (Go proxy
// @latest v0.3.2). The accepted risk is the linked but never-constructed
// BatchProcessor; the moment it could be constructed, this test fails.
//
// The package graph is the host platform's; the scan also reads files that
// build constraints exclude, so a platform-specific caller in an already
// linked package is still found.
func TestOTelLogBatchProcessorStaysUnreachable(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not available on PATH")
	}

	root := moduleRoot(t)
	for _, moduleDir := range claircoreModuleDirs(root) {
		name := filepath.Base(moduleDir)
		if moduleDir == root {
			name = "root"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			auditedVersion, ok := auditedOTelSDKLogVersions()[name]
			if !ok {
				t.Fatalf("module %q has no audited sdk/log version", name)
			}

			assertOTelSDKLogVersion(t, moduleDir, auditedVersion)

			packages := listDependencyPackages(t, moduleDir)
			assertOTelSDKLogImporters(t, packages)
			assertNoOTelBatchEntryPointCallers(t, packages)
		})
	}
}

// TestFindEntryPointReferencesDetectsCallers proves the scanner finds a direct
// call, a function value, a renamed import and a dot import, and honours the
// allowed enclosing function, so an empty result is a real verdict.
func TestFindEntryPointReferencesDetectsCallers(t *testing.T) {
	t.Parallel()

	src := `package demo

import (
	logger "github.com/kubescape/go-logger"
	up "github.com/uptrace/uptrace-go/uptrace"
	. "github.com/kubescape/go-logger"
)

func InitOtel() { up.ConfigureOpentelemetry() }

func a() { logger.InitOtel() }

var b = logger.InitOtel
`

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "demo.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	names := map[string]string{kubescapeLoggerPath: "logger", uptracePackagePath: "uptrace"}

	got := findEntryPointReferences(fset, file, names, nil)
	if len(got) != 4 {
		t.Fatalf("expected 4 references (dot import, InitOtel body, call, value), got %d: %v", len(got), got)
	}

	allowed := map[string]string{uptracePackagePath: "InitOtel"}

	got = findEntryPointReferences(fset, file, names, allowed)
	if len(got) != 3 {
		t.Fatalf("expected 3 references once the InitOtel body is allowed, got %d: %v", len(got), got)
	}
}

func assertOTelSDKLogVersion(t *testing.T, moduleDir, auditedVersion string) {
	t.Helper()

	out, err := runGoCommand(t.Context(), moduleDir, "list", "-m", "-json", otelSDKLogModulePath)
	if err != nil {
		t.Fatalf("read %q version: %v", otelSDKLogModulePath, err)
	}

	actual := goListModule{}

	err = json.Unmarshal(out, &actual)
	if err != nil {
		t.Fatalf("decode %q module metadata: %v", otelSDKLogModulePath, err)
	}

	if actual.Replace != nil || actual.Version != auditedVersion {
		t.Fatalf(
			"%s moved from audited %s to %+v in %q: re-establish the GHSA-hjf4-fphr-2h65 verdict "+
				"(#7375), and delete this guard once the version is v0.21.0 or later",
			otelSDKLogModulePath, auditedVersion, actual, moduleDir,
		)
	}
}

func listDependencyPackages(t *testing.T, moduleDir string) []goListPackage {
	t.Helper()

	out, err := runGoCommand(t.Context(), moduleDir, "list", "-deps", "-json", "./...")
	if err != nil {
		t.Fatalf("list dependency packages: %v", err)
	}

	var packages []goListPackage

	decoder := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var pkg goListPackage

		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("decode go list output for %q: %v", moduleDir, err)
		}

		packages = append(packages, pkg)
	}

	return packages
}

func assertOTelSDKLogImporters(t *testing.T, packages []goListPackage) {
	t.Helper()

	audited := auditedOTelSDKLogImporters()

	var unexpected []string

	found := false

	for _, pkg := range packages {
		if pkg.ImportPath == otelSDKLogModulePath {
			found = true
		}

		for _, imported := range pkg.Imports {
			if imported == otelSDKLogModulePath && !audited[pkg.ImportPath] {
				unexpected = append(unexpected, pkg.ImportPath)
			}
		}
	}

	if !found {
		t.Fatalf("%s is no longer linked: re-establish the #7375 verdict and delete this guard", otelSDKLogModulePath)
	}

	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Fatalf("unaudited packages import %s (#7375): %v", otelSDKLogModulePath, unexpected)
	}
}

func assertNoOTelBatchEntryPointCallers(t *testing.T, packages []goListPackage) {
	t.Helper()

	packageNames := make(map[string]string, len(packages))
	for _, pkg := range packages {
		packageNames[pkg.ImportPath] = pkg.Name
	}

	scanned := 0

	var references []string

	for _, pkg := range packages {
		if pkg.Standard || !importsEntryPoint(pkg) {
			continue
		}

		scanned++

		refs, err := scanPackageForEntryPoints(pkg, packageNames)
		if err != nil {
			t.Fatalf("scan %s: %v", pkg.ImportPath, err)
		}

		references = append(references, refs...)
	}

	if scanned == 0 {
		t.Fatal("no package imports an OTel batch entry point, so the scan examined nothing")
	}

	if len(references) > 0 {
		t.Fatalf("OTel log BatchProcessor entry points are referenced (GHSA-hjf4-fphr-2h65, #7375):\n%s",
			strings.Join(references, "\n"))
	}
}

func importsEntryPoint(pkg goListPackage) bool {
	for _, imported := range pkg.Imports {
		if _, ok := otelBatchEntryPoints()[imported]; ok {
			return true
		}
	}

	return false
}

func scanPackageForEntryPoints(pkg goListPackage, packageNames map[string]string) ([]string, error) {
	files := append(append(append([]string{}, pkg.GoFiles...), pkg.CgoFiles...), pkg.IgnoredGoFiles...)
	fset := token.NewFileSet()
	allowed := allowedEntryPointReferences()[pkg.ImportPath]

	var references []string

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		references = append(references, findEntryPointReferences(fset, file, packageNames, allowed)...)
	}

	return references, nil
}

// findEntryPointReferences reports every selector naming an entry point on an
// import of its package, and every dot import of such a package, except
// references inside the function allowed for that package.
func findEntryPointReferences(
	fset *token.FileSet,
	file *ast.File,
	packageNames map[string]string,
	allowed map[string]string,
) []string {
	localNames, references := entryPointImports(fset, file, packageNames)

	for _, decl := range file.Decls {
		references = append(references, selectorReferences(fset, decl, localNames, allowed)...)
	}

	return references
}

// entryPointImports maps the local name of every import of an entry-point
// package to its path, and reports each dot import of one, which no selector
// check could see.
func entryPointImports(
	fset *token.FileSet,
	file *ast.File,
	packageNames map[string]string,
) (map[string]string, []string) {
	localNames := map[string]string{}

	var references []string

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || otelBatchEntryPoints()[path] == "" {
			continue
		}

		local := packageNames[path]
		if spec.Name != nil {
			local = spec.Name.Name
		}

		if local == "." {
			references = append(references, fmt.Sprintf("%s: dot import of %s", fset.Position(spec.Pos()), path))

			continue
		}

		localNames[local] = path
	}

	return localNames, references
}

// selectorReferences reports every selector in decl naming an entry point on
// one of localNames, unless decl is the function allowed for that package.
func selectorReferences(
	fset *token.FileSet,
	decl ast.Decl,
	localNames map[string]string,
	allowed map[string]string,
) []string {
	enclosing := ""
	if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Recv == nil {
		enclosing = fn.Name.Name
	}

	var references []string

	ast.Inspect(decl, func(node ast.Node) bool {
		sel, isSelector := node.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}

		ident, isIdent := sel.X.(*ast.Ident)
		if !isIdent {
			return true
		}

		path, imported := localNames[ident.Name]
		if !imported || sel.Sel.Name != otelBatchEntryPoints()[path] {
			return true
		}

		allowedFunc, isAllowed := allowed[path]
		if !isAllowed || allowedFunc != enclosing {
			references = append(references, fmt.Sprintf("%s: %s.%s", fset.Position(sel.Pos()), path, sel.Sel.Name))
		}

		return true
	})

	return references
}

package kubescape_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const (
	otelSDKLogModulePath  = "go.opentelemetry.io/otel/sdk/log"
	uptraceModulePath     = "github.com/uptrace/uptrace-go"
	uptracePackagePath    = "github.com/uptrace/uptrace-go/uptrace"
	kubescapeLoggerPath   = "github.com/kubescape/go-logger"
	otlpLogHTTPModulePath = "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
)

// goListPackageFields limits `go list -json` to the fields goListPackage reads: the full record is
// about 69 MB per module and platform, and the audit lists twelve of them.
const goListPackageFields = "ImportPath,Name,Dir,Standard,Module,GoFiles,CgoFiles,IgnoredGoFiles,Imports"

// goListPackage mirrors the Go command's exported JSON field names.
type goListPackage struct {
	ImportPath     string        `json:"ImportPath"`     //nolint:tagliatelle // Go command output contract.
	Name           string        `json:"Name"`           //nolint:tagliatelle // Go command output contract.
	Dir            string        `json:"Dir"`            //nolint:tagliatelle // Go command output contract.
	Standard       bool          `json:"Standard"`       //nolint:tagliatelle // Go command output contract.
	Module         *goListModule `json:"Module"`         //nolint:tagliatelle // Go command output contract.
	GoFiles        []string      `json:"GoFiles"`        //nolint:tagliatelle // Go command output contract.
	CgoFiles       []string      `json:"CgoFiles"`       //nolint:tagliatelle // Go command output contract.
	IgnoredGoFiles []string      `json:"IgnoredGoFiles"` //nolint:tagliatelle // Go command output contract.
	Imports        []string      `json:"Imports"`        //nolint:tagliatelle // Go command output contract.
}

// auditedOTelModuleVersions pins, per shipped module, the releases the
// reachability verdict was established against. The advisory
// GHSA-hjf4-fphr-2h65 (BatchProcessor busy-spin, fixed in v0.21.0) affects the
// go.opentelemetry.io/otel/sdk/log release, and no fix can be adopted until
// uptrace's otelutil compiles against otel/log v0.21 (#7375). A move to v0.21.0
// or later makes this guard and its risk acceptance obsolete: delete both then.
// uptrace-go and kubescape's go-logger are pinned too, because the entry points
// below were read from these releases: a new release may construct the
// processor from init or through another exported function this scan does not
// name, so any change to either re-opens the verdict.
func auditedOTelModuleVersions() map[string]map[string]string {
	audited := map[string]string{
		otelSDKLogModulePath:  "v0.19.0",
		uptraceModulePath:     "v1.37.0",
		kubescapeLoggerPath:   "v0.0.25",
		otlpLogHTTPModulePath: "v0.19.0",
	}

	return map[string]map[string]string{
		"root":    audited,
		"desktop": audited,
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
//
//	@latest	v0.3.2). The accepted risk is the linked but never-constructed
//
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

	// The CLI and the desktop app are both built from the root module (ADR 0007); they differ only
	// in build tags and CGO, so each build is listed from the root with its own settings.
	root := moduleRoot(t)
	for _, name := range []string{"root", "desktop"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			auditModuleOTelReachability(t, name, root)
		})
	}
}

// auditModuleOTelReachability pins the audited module versions for one shipped module, then checks
// its package graph on every shipped platform.
func auditModuleOTelReachability(t *testing.T, name, moduleDir string) {
	t.Helper()

	auditedVersions, ok := auditedOTelModuleVersions()[name]
	if !ok {
		t.Fatalf("module %q has no audited OTel module versions", name)
	}

	cgo := moduleCGO()[name]
	if cgo == "" {
		t.Fatalf("module %q has no CGO setting", name)
	}

	for modulePath, auditedVersion := range auditedVersions {
		assertAuditedModuleVersion(t, moduleDir, modulePath, auditedVersion)
	}

	// In vendor mode the build reads module sources from vendor/, which `go list -m` does not reflect,
	// so the audited versions would no longer say what is compiled.
	_, err := os.Stat(filepath.Join(moduleDir, "vendor", "modules.txt"))
	if err == nil {
		t.Fatalf(
			"module %q is vendored: re-establish the #7375 verdict against the vendored sources",
			name,
		)
	}

	linked, scanned := false, false

	for _, target := range shippedPlatforms() {
		packages := listDependencyPackages(t, moduleDir, target, cgo, moduleBuildTags()[name])
		assertAuditedPackageModules(t, packages, auditedVersions, target.String())
		linked = assertOTelSDKLogImporters(t, packages, target.String()) || linked
		scanned = assertNoOTelBatchEntryPointCallers(t, packages, target.String()) || scanned
	}

	if !linked {
		t.Fatalf(
			"%s is no longer linked on any shipped platform: re-establish the #7375 verdict and delete this guard",
			otelSDKLogModulePath,
		)
	}

	if !scanned {
		t.Fatal(
			"no package imports an OTel batch entry point on any shipped platform, so the scan examined nothing",
		)
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
		t.Fatalf(
			"expected 4 references (dot import, InitOtel body, call, value), got %d: %v",
			len(got),
			got,
		)
	}

	allowed := map[string]string{uptracePackagePath: "InitOtel"}

	got = findEntryPointReferences(fset, file, names, allowed)
	if len(got) != 3 {
		t.Fatalf(
			"expected 3 references once the InitOtel body is allowed, got %d: %v",
			len(got),
			got,
		)
	}
}

// TestImportsEntryPointReadsBuildConstrainedFiles proves a package that imports
// an entry point only from a file the host's build constraints exclude is still
// selected for the scan, and that a package importing none is not.
func TestImportsEntryPointReadsBuildConstrainedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	windowsOnly := "//go:build windows\n\npackage demo\n\nimport logger \"" + kubescapeLoggerPath + "\"\n\n" +
		"func init() { logger.InitOtel() }\n"
	unrelated := "//go:build windows\n\npackage demo\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n"

	for name, src := range map[string]string{"otel_windows.go": windowsOnly, "other_windows.go": unrelated} {
		err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600)
		if err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	selected, err := importsEntryPoint(
		goListPackage{Dir: dir, IgnoredGoFiles: []string{"otel_windows.go"}},
	)
	if err != nil || !selected {
		t.Fatalf(
			"a Windows-only entry-point import was not selected for the scan: selected=%v err=%v",
			selected,
			err,
		)
	}

	selected, err = importsEntryPoint(
		goListPackage{Dir: dir, IgnoredGoFiles: []string{"other_windows.go"}},
	)
	if err != nil || selected {
		t.Fatalf(
			"a package importing no entry point was selected: selected=%v err=%v",
			selected,
			err,
		)
	}
}

func assertAuditedModuleVersion(t *testing.T, moduleDir, modulePath, auditedVersion string) {
	t.Helper()

	//nolint:gosec // G204: a fixed go subcommand and an audited module path.
	cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-json", modulePath)
	cmd.Dir = moduleDir
	cmd.Env = auditGoEnv()

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("read %q version: %v\n%s", modulePath, err,
			boundGoStderr(stderr.String(), goCommandStderrLimit))
	}

	actual := goListModule{}

	err = json.Unmarshal(out, &actual)
	if err != nil {
		t.Fatalf("decode %q module metadata: %v", modulePath, err)
	}

	if actual.Replace != nil || actual.Version != auditedVersion {
		t.Fatalf(
			"%s moved from audited %s to %+v in %q: re-establish the GHSA-hjf4-fphr-2h65 verdict "+
				"(#7375), and delete this guard once %s is v0.21.0 or later",
			modulePath, auditedVersion, actual, moduleDir, otelSDKLogModulePath,
		)
	}
}

// platform is one GOOS/GOARCH pair a module is built for.
type platform struct {
	goos   string
	goarch string
}

func (p platform) String() string { return p.goos + "/" + p.goarch }

// shippedPlatforms is every GOOS/GOARCH pair either module ships on: the CLI release matrix
// (.goreleaser.yaml: darwin, linux and windows on amd64 and arm64, less darwin/amd64), which also
// covers every desktop build (darwin/arm64 cask, linux/amd64 and windows/amd64 downloads). The
// package graph differs per platform, so a caller present only in, say, the windows/arm64 graph is
// invisible to any other listing. An unshipped pair is left out: its graph could fail the audit
// for a binary nobody receives. TestShippedPlatformsCoverReleaseMatrices keeps this list equal to
// the release matrices in both directions.
func shippedPlatforms() []platform {
	return []platform{
		{goos: "darwin", goarch: "arm64"},
		{goos: "linux", goarch: "amd64"},
		{goos: "linux", goarch: "arm64"},
		{goos: "windows", goarch: "amd64"},
		{goos: "windows", goarch: "arm64"},
	}
}

// moduleCGO is the CGO setting each shipped module is built with: the CLI release disables CGO,
// while the desktop app needs it for its webview, and without it the desktop graph cannot be listed.
func moduleCGO() map[string]string {
	return map[string]string{"root": "0", "desktop": "1"}
}

// moduleBuildTags is the build tag set each shipped build is compiled with: the desktop app shares
// the root module and opts in with the `desktop` tag (ADR 0007), so without it the desktop graph is
// the CLI's.
func moduleBuildTags() map[string]string {
	return map[string]string{"root": "", "desktop": "desktop"}
}

// auditGoEnv is the caller's environment with every graph-affecting Go setting the release
// configuration does not control cleared: GOFLAGS (tags, -modfile, -mod), GOEXPERIMENT (which adds
// goexperiment.* tags), the user go env file and a workspace file would otherwise make the audit
// list a graph the release never builds.
func auditGoEnv(extra ...string) []string {
	env := append(os.Environ(), "GOFLAGS=", "GOENV=off", "GOWORK=off", "GOEXPERIMENT=")

	return append(env, extra...)
}

func listDependencyPackages(
	t *testing.T,
	moduleDir string,
	target platform,
	cgo string,
	tags string,
) []goListPackage {
	t.Helper()

	//nolint:gosec // G204: a fixed go subcommand; tags come from moduleBuildTags.
	cmd := exec.CommandContext(
		t.Context(),
		"go",
		"list",
		"-deps",
		"-tags="+tags,
		"-json="+goListPackageFields,
		"./...",
	)
	cmd.Dir = moduleDir

	cmd.Env = auditGoEnv("GOOS="+target.goos, "GOARCH="+target.goarch, "CGO_ENABLED="+cgo)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("list dependency packages for %s: %v\n%s", target, err,
			boundGoStderr(stderr.String(), goCommandStderrLimit))
	}

	var packages []goListPackage

	decoder := json.NewDecoder(bytes.NewReader(out))

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

// auditedPackageModules maps every package the verdict trusts by import path (sdk/log, its audited
// importers and the entry-point packages) to the module that must supply it. Go resolves a package
// to the longest matching module path, so a new child module could supply an allowlisted path
// while the parent module stays at its audited version.
func auditedPackageModules() map[string]string {
	return map[string]string{
		otelSDKLogModulePath:                          otelSDKLogModulePath,
		uptracePackagePath:                            uptraceModulePath,
		kubescapeLoggerPath:                           kubescapeLoggerPath,
		otlpLogHTTPModulePath:                         otlpLogHTTPModulePath,
		otlpLogHTTPModulePath + "/internal/transform": otlpLogHTTPModulePath,
	}
}

// assertAuditedPackageModules fails when a trusted package comes from any module, version or
// replacement other than the audited one.
func assertAuditedPackageModules(
	t *testing.T,
	packages []goListPackage,
	auditedVersions map[string]string,
	target string,
) {
	t.Helper()

	expected := auditedPackageModules()

	for _, pkg := range packages {
		modulePath, trusted := expected[pkg.ImportPath]
		if !trusted {
			continue
		}

		module := pkg.Module
		if module == nil || module.Path != modulePath || module.Replace != nil ||
			module.Version != auditedVersions[modulePath] {
			t.Fatalf(
				"%s on %s comes from %+v, not the audited %s@%s: re-establish the #7375 verdict",
				pkg.ImportPath, target, module, modulePath, auditedVersions[modulePath],
			)
		}
	}
}

// assertOTelSDKLogImporters fails on any unaudited importer of sdk/log and reports whether the
// graph links sdk/log at all.
func assertOTelSDKLogImporters(t *testing.T, packages []goListPackage, target string) bool {
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

	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Fatalf(
			"unaudited packages import %s on %s (#7375): %v",
			otelSDKLogModulePath,
			target,
			unexpected,
		)
	}

	return found
}

// assertNoOTelBatchEntryPointCallers fails on any entry-point reference outside the allowed places and
// reports whether any package was scanned.
func assertNoOTelBatchEntryPointCallers(
	t *testing.T,
	packages []goListPackage,
	target string,
) bool {
	t.Helper()

	packageNames := make(map[string]string, len(packages))
	for _, pkg := range packages {
		packageNames[pkg.ImportPath] = pkg.Name
	}

	scanned := 0

	var references []string

	for path, name := range entryPointPackageNames() {
		if packageNames[path] == "" {
			packageNames[path] = name
		}
	}

	for _, pkg := range packages {
		if pkg.Standard {
			continue
		}

		imports, err := importsEntryPoint(pkg)
		if err != nil {
			t.Fatalf("read imports of %s: %v", pkg.ImportPath, err)
		}

		if !imports {
			continue
		}

		scanned++

		refs, err := scanPackageForEntryPoints(pkg, packageNames)
		if err != nil {
			t.Fatalf("scan %s: %v", pkg.ImportPath, err)
		}

		references = append(references, refs...)
	}

	if len(references) > 0 {
		t.Fatalf(
			"OTel log BatchProcessor entry points are referenced on %s (GHSA-hjf4-fphr-2h65, #7375):\n%s",
			target,
			strings.Join(references, "\n"),
		)
	}

	return scanned > 0
}

// entryPointPackageNames gives each entry-point package's declared name, for a
// package that imports one only from a file the host's build constraints
// exclude: the host graph then need not contain the entry-point package at all.
func entryPointPackageNames() map[string]string {
	return map[string]string{
		kubescapeLoggerPath: "logger",
		uptracePackagePath:  "uptrace",
	}
}

// importsEntryPoint reports whether pkg imports an entry-point package from any
// non-test file, including files the host's build constraints exclude: go list
// derives Imports from the active files only, so a caller in, say, a
// Windows-only file of a package that imports nothing else relevant would
// otherwise never be scanned.
func importsEntryPoint(pkg goListPackage) (bool, error) {
	for _, imported := range pkg.Imports {
		if _, isEntryPoint := otelBatchEntryPoints()[imported]; isEntryPoint {
			return true, nil
		}
	}

	fset := token.NewFileSet()

	for _, name := range pkg.IgnoredGoFiles {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.ImportsOnly)
		if err != nil {
			return false, fmt.Errorf("parse imports of %s: %w", name, err)
		}

		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return false, fmt.Errorf("unquote import in %s: %w", name, err)
			}

			if _, isEntryPoint := otelBatchEntryPoints()[path]; isEntryPoint {
				return true, nil
			}
		}
	}

	return false, nil
}

func scanPackageForEntryPoints(
	pkg goListPackage,
	packageNames map[string]string,
) ([]string, error) {
	files := append(
		append(append([]string{}, pkg.GoFiles...), pkg.CgoFiles...),
		pkg.IgnoredGoFiles...)
	fset := token.NewFileSet()
	allowed := allowedEntryPointReferences()[pkg.ImportPath]

	var references []string

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(
			fset,
			filepath.Join(pkg.Dir, name),
			nil,
			parser.SkipObjectResolution,
		)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		references = append(
			references,
			findEntryPointReferences(fset, file, packageNames, allowed)...)
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
			references = append(
				references,
				fmt.Sprintf("%s: dot import of %s", fset.Position(spec.Pos()), path),
			)

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
			references = append(
				references,
				fmt.Sprintf("%s: %s.%s", fset.Position(sel.Pos()), path, sel.Sel.Name),
			)
		}

		return true
	})

	return references
}

// releaseTarget is one GOOS/GOARCH pair a release configuration builds.
type releaseTarget struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

// TestShippedPlatformsCoverReleaseMatrices pins shippedPlatforms to the release configurations: every
// target the CLI release (.goreleaser.yaml), the macOS desktop release (.goreleaser.desktop.yaml) and
// the Linux/Windows desktop builds (cd.yaml) produce must be audited, so a target added to a release
// cannot ship a graph the reachability audit never listed.
func TestShippedPlatformsCoverReleaseMatrices(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	audited := map[string]bool{}

	for _, target := range shippedPlatforms() {
		audited[target.String()] = true
	}

	targets := append(goreleaserTargets(t, filepath.Join(root, ".goreleaser.yaml")),
		goreleaserTargets(t, filepath.Join(root, ".goreleaser.desktop.yaml"))...)
	targets = append(
		targets,
		desktopWorkflowTargets(t, filepath.Join(root, ".github", "workflows", "cd.yaml"))...)

	if len(targets) == 0 {
		t.Fatal("no release target was read, so the comparison examined nothing")
	}

	released := map[string]bool{}

	for _, target := range targets {
		key := target.GOOS + "/" + target.GOARCH
		released[key] = true

		if !audited[key] {
			t.Errorf("release target %s is not in shippedPlatforms (#7375)", key)
		}
	}

	for key := range audited {
		if !released[key] {
			t.Errorf("shippedPlatforms audits %s, which no release builds (#7375)", key)
		}
	}
}

// goreleaserTargets expands every build's goos x goarch matrix in a GoReleaser file, less its ignores.
func goreleaserTargets(t *testing.T, path string) []releaseTarget {
	t.Helper()

	var config struct {
		Builds []struct {
			GOOS   []string        `json:"goos"`
			GOARCH []string        `json:"goarch"`
			Ignore []releaseTarget `json:"ignore"`
		} `json:"builds"`
	}

	readYAML(t, path, &config)

	var targets []releaseTarget

	for _, build := range config.Builds {
		if len(build.GOOS) == 0 || len(build.GOARCH) == 0 {
			t.Fatalf("%s has a build without an explicit goos and goarch list", path)
		}

		for _, goos := range build.GOOS {
			for _, goarch := range build.GOARCH {
				target := releaseTarget{GOOS: goos, GOARCH: goarch}
				if !slices.Contains(build.Ignore, target) {
					targets = append(targets, target)
				}
			}
		}
	}

	return targets
}

// desktopWorkflowTargets reads the desktop job's build matrix from the CD workflow.
func desktopWorkflowTargets(t *testing.T, path string) []releaseTarget {
	t.Helper()

	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []struct {
						GOOS string `json:"goos"`
						Arch string `json:"arch"`
					} `json:"include"`
				} `json:"matrix"`
			} `json:"strategy"`
		} `json:"jobs"`
	}

	readYAML(t, path, &workflow)

	include := workflow.Jobs["desktop"].Strategy.Matrix.Include
	if len(include) == 0 {
		t.Fatalf("%s has no desktop build matrix", path)
	}

	targets := make([]releaseTarget, 0, len(include))
	for _, entry := range include {
		targets = append(targets, releaseTarget{GOOS: entry.GOOS, GOARCH: entry.Arch})
	}

	return targets
}

func readYAML(t *testing.T, path string, into any) {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // G304: fixed release configuration paths.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	err = yaml.Unmarshal(data, into)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

// TestModuleCGOMatchesReleaseBuilds pins moduleCGO to the CGO_ENABLED each module's GoReleaser
// configuration builds with: a file selected only by the other CGO mode belongs to a graph the
// audit would otherwise never list. The Linux and Windows desktop builds in cd.yaml are native
// `go build` runs, where Go's own default is CGO enabled, matching the desktop setting here.
func TestModuleCGOMatchesReleaseBuilds(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	for name, file := range map[string]string{"root": ".goreleaser.yaml", "desktop": ".goreleaser.desktop.yaml"} {
		released := goreleaserCGO(t, filepath.Join(root, file))
		if moduleCGO()[name] != released {
			t.Errorf("moduleCGO()[%q] = %q, but %s builds with CGO_ENABLED=%s (#7375)",
				name, moduleCGO()[name], file, released)
		}
	}
}

// TestModuleBuildTagsMatchReleaseBuilds pins moduleBuildTags to the build tags each GoReleaser
// configuration compiles with: a tag-gated file belongs to a graph the audit would otherwise never
// list.
func TestModuleBuildTagsMatchReleaseBuilds(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	for name, file := range map[string]string{"root": ".goreleaser.yaml", "desktop": ".goreleaser.desktop.yaml"} {
		released := goreleaserTags(t, filepath.Join(root, file))
		if moduleBuildTags()[name] != released {
			t.Errorf("moduleBuildTags()[%q] = %q, but %s builds with tags %q (#7375)",
				name, moduleBuildTags()[name], file, released)
		}
	}
}

// goreleaserTags returns the comma-joined build tags every build in a GoReleaser file sets; builds
// that disagree fail the test rather than guess.
func goreleaserTags(t *testing.T, path string) string {
	t.Helper()

	var config struct {
		Builds []struct {
			Tags []string `json:"tags"`
		} `json:"builds"`
	}

	readYAML(t, path, &config)

	if len(config.Builds) == 0 {
		t.Fatalf("%s has no build", path)
	}

	value := strings.Join(config.Builds[0].Tags, ",")

	for _, build := range config.Builds[1:] {
		if joined := strings.Join(build.Tags, ","); joined != value {
			t.Fatalf("%s: every build must set the same tags, got %q and %q", path, value, joined)
		}
	}

	return value
}

// goreleaserCGO returns the CGO_ENABLED value every build in a GoReleaser file sets; a build that
// sets none, or builds that disagree, fail the test rather than guess.
func goreleaserCGO(t *testing.T, path string) string {
	t.Helper()

	var config struct {
		Builds []struct {
			Env []string `json:"env"`
		} `json:"builds"`
	}

	readYAML(t, path, &config)

	value := ""

	for _, build := range config.Builds {
		found := ""

		for _, entry := range build.Env {
			if cgo, ok := strings.CutPrefix(entry, "CGO_ENABLED="); ok {
				found = cgo
			}
		}

		if found == "" || (value != "" && found != value) {
			t.Fatalf(
				"%s: every build must set one CGO_ENABLED value, got %q after %q",
				path,
				found,
				value,
			)
		}

		value = found
	}

	if value == "" {
		t.Fatalf("%s has no build", path)
	}

	return value
}

// auditedReleaseConfigDigests pins the release build configuration the #7375 verdict was
// established against: both GoReleaser files, the desktop job in cd.yaml and the setup action it
// uses. The platform and CGO tests above check what those files say today; this catches every
// other way a release can change the graph it ships (build tags, flags, a build dir pointing at
// another module, a CGO override inside a run line), because ANY change to them re-opens the audit.
// To update: re-run the reachability audit for the new configuration, then paste the new digests.
//
//nolint:lll // file-path keys and 64-character SHA-256 digests do not wrap
func auditedReleaseConfigDigests() map[string]string {
	return map[string]string{
		".goreleaser.yaml":                                   "603b04ca07558b8e1acb3d9c4e00e3c49ceaa10ea9f5af0ed2f84f01f96496c2",
		".goreleaser.desktop.yaml":                           "46eef12c0c592f5fae1a76082d897a1f63099a4a87b268ed7a39629c8e7c4eb6",
		".github/actions/setup-desktop-build/action.yml":     "09d319886697e84b880a9744cacb0928daa9a6998f24600cf09183e152592c6a",
		"scripts/stage-webui.sh":                             "5b3d7b0fa8b237f77ee9a88e6807e070c1b35c97df3d5f17357b3ecb8b2938a6",
		".github/actions/free-disk-space/free-disk-space.sh": "2dd12fcf3779137ca1cb5f21194947f6a10d3f94f8fb438a1a29ec9021fbdc30",
		".github/workflows/cd.yaml#without-uses":             "12907f450e439856e5f369ed804a3fea5f04e64870f39d8bb3efa105ad2204ba",
	}
}

// TestReleaseBuildConfigMatchesAudit fails when any audited release build configuration changes.
func TestReleaseBuildConfigMatchesAudit(t *testing.T) {
	t.Parallel()

	actual := releaseConfigDigests(t, moduleRoot(t))
	audited := auditedReleaseConfigDigests()

	// A hashed input without an audited digest would otherwise never be compared.
	for name := range actual {
		if _, ok := audited[name]; !ok {
			t.Errorf("%s is hashed but has no audited digest in auditedReleaseConfigDigests", name)
		}
	}

	for name, want := range audited {
		if actual[name] != want {
			t.Errorf(
				"%s changed (digest %s, audited %s): re-establish the GHSA-hjf4-fphr-2h65 verdict "+
					"for the new release configuration (#7375), then update auditedReleaseConfigDigests",
				name,
				actual[name],
				want,
			)
		}
	}
}

// releaseConfigDigests hashes each audited release configuration: whole files, and for cd.yaml the
// whole workflow (workflow-level env such as GOFLAGS reaches every release job) canonicalised through
// JSON with every `uses:` value removed, so formatting and action-version bumps do not count.
func releaseConfigDigests(t *testing.T, root string) map[string]string {
	t.Helper()

	digests := map[string]string{}

	for _, name := range []string{
		".goreleaser.yaml", ".goreleaser.desktop.yaml", ".github/actions/setup-desktop-build/action.yml",
		"scripts/stage-webui.sh",
		".github/actions/free-disk-space/free-disk-space.sh",
	} {
		path := filepath.Join(root, name)

		data, err := os.ReadFile(path) //nolint:gosec // G304: fixed release config paths.
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		// A CRLF checkout (Windows, core.autocrlf) must not change the digest of unchanged content.
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		digests[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}

	var workflow map[string]any

	readYAML(t, filepath.Join(root, ".github", "workflows", "cd.yaml"), &workflow)

	if _, ok := workflow["jobs"]; !ok {
		t.Fatal("cd.yaml has no jobs")
	}

	encoded, err := json.Marshal(withoutActionPins(workflow))
	if err != nil {
		t.Fatalf("encode cd.yaml: %v", err)
	}

	digests[".github/workflows/cd.yaml#without-uses"] = fmt.Sprintf("%x", sha256.Sum256(encoded))

	return digests
}

// withoutActionPins returns a copy of a decoded workflow with only the VERSION of each remote action
// removed (`owner/action@sha` becomes `owner/action`): a version bump cannot change the Go graph a
// release builds, while swapping in another action, or pointing at another local action, can.
func withoutActionPins(node any) any {
	switch value := node.(type) {
	case map[string]any:
		copied := make(map[string]any, len(value))

		for key, child := range value {
			if action, isString := child.(string); key == "uses" && isString {
				copied[key] = actionIdentity(action)

				continue
			}

			copied[key] = withoutActionPins(child)
		}

		return copied
	case []any:
		copied := make([]any, len(value))
		for index, child := range value {
			copied[index] = withoutActionPins(child)
		}

		return copied
	default:
		return node
	}
}

// actionIdentity keeps a local action's path whole and drops a remote action's @version.
func actionIdentity(action string) string {
	if strings.HasPrefix(action, "./") {
		return action
	}

	name, _, _ := strings.Cut(action, "@")

	return name
}

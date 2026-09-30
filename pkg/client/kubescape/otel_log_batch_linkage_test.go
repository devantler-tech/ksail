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
const goListPackageFields = "ImportPath,Name,Dir,Standard,GoFiles,CgoFiles,IgnoredGoFiles,Imports"

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

	root := moduleRoot(t)
	for _, moduleDir := range claircoreModuleDirs(root) {
		name := filepath.Base(moduleDir)
		if moduleDir == root {
			name = "root"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()
			auditModuleOTelReachability(t, name, moduleDir)
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

	linked, scanned := false, false

	for _, target := range shippedPlatforms() {
		packages := listDependencyPackages(t, moduleDir, target, cgo)
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

	out, err := runGoCommand(t.Context(), moduleDir, "list", "-m", "-json", modulePath)
	if err != nil {
		t.Fatalf("read %q version: %v", modulePath, err)
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

// shippedPlatforms is every GOOS/GOARCH pair either module can ship on: the CLI release matrix
// (.goreleaser.yaml: darwin, linux and windows on amd64 and arm64, less darwin/amd64) plus
// darwin/amd64, so the desktop app is covered too. The package graph differs per platform, so a
// caller present only in, say, the windows/arm64 graph is invisible to any other listing.
func shippedPlatforms() []platform {
	platforms := make([]platform, 0, 6)

	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			platforms = append(platforms, platform{goos: goos, goarch: goarch})
		}
	}

	return platforms
}

// moduleCGO is the CGO setting each shipped module is built with: the CLI release disables CGO,
// while the desktop app needs it for its webview, and without it the desktop graph cannot be listed.
func moduleCGO() map[string]string {
	return map[string]string{"root": "0", "desktop": "1"}
}

func listDependencyPackages(
	t *testing.T,
	moduleDir string,
	target platform,
	cgo string,
) []goListPackage {
	t.Helper()

	cmd := exec.CommandContext(
		t.Context(),
		"go",
		"list",
		"-deps",
		"-json="+goListPackageFields,
		"./...",
	)
	cmd.Dir = moduleDir

	cmd.Env = append(os.Environ(), "GOOS="+target.goos, "GOARCH="+target.goarch, "CGO_ENABLED="+cgo)

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

	for _, target := range targets {
		if !audited[target.GOOS+"/"+target.GOARCH] {
			t.Errorf(
				"release target %s/%s is not in shippedPlatforms (#7375)",
				target.GOOS,
				target.GOARCH,
			)
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
func auditedReleaseConfigDigests() map[string]string {
	return map[string]string{
		".goreleaser.yaml":                               "603b04ca07558b8e1acb3d9c4e00e3c49ceaa10ea9f5af0ed2f84f01f96496c2",
		".goreleaser.desktop.yaml":                       "be6f45e10f4f608db285d641ce166714a404aaa2d31aacf9479679fbdb58df11",
		".github/actions/setup-desktop-build/action.yml": "d4b17b1a6442ffc2e499419153db96c75fff26c836027ed8cc00f43dea5a2559",
		".github/workflows/cd.yaml#jobs.desktop":         "3f68fcec1219196b540664b44b8d44d257f547e582ae956db9214f074bd0f32a",
	}
}

// TestReleaseBuildConfigMatchesAudit fails when any audited release build configuration changes.
func TestReleaseBuildConfigMatchesAudit(t *testing.T) {
	t.Parallel()

	actual := releaseConfigDigests(t, moduleRoot(t))

	for name, want := range auditedReleaseConfigDigests() {
		if actual[name] != want {
			t.Errorf("%s changed (digest %s, audited %s): re-establish the GHSA-hjf4-fphr-2h65 verdict "+
				"for the new release configuration (#7375), then update auditedReleaseConfigDigests",
				name, actual[name], want)
		}
	}
}

// releaseConfigDigests hashes each audited release configuration: whole files, and for cd.yaml only
// the desktop job, canonicalised through JSON so unrelated jobs and formatting do not count.
func releaseConfigDigests(t *testing.T, root string) map[string]string {
	t.Helper()

	digests := map[string]string{}

	for _, name := range []string{
		".goreleaser.yaml", ".goreleaser.desktop.yaml", ".github/actions/setup-desktop-build/action.yml",
	} {
		data, err := os.ReadFile(filepath.Join(root, name)) //nolint:gosec // G304: fixed release config paths.
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		digests[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}

	var workflow struct {
		Jobs map[string]json.RawMessage `json:"jobs"`
	}

	readYAML(t, filepath.Join(root, ".github", "workflows", "cd.yaml"), &workflow)

	job, ok := workflow.Jobs["desktop"]
	if !ok {
		t.Fatal("cd.yaml has no desktop job")
	}

	var canonical any

	err := json.Unmarshal(job, &canonical)
	if err != nil {
		t.Fatalf("decode the desktop job: %v", err)
	}

	encoded, err := json.Marshal(canonical)
	if err != nil {
		t.Fatalf("encode the desktop job: %v", err)
	}

	digests[".github/workflows/cd.yaml#jobs.desktop"] = fmt.Sprintf("%x", sha256.Sum256(encoded))

	return digests
}

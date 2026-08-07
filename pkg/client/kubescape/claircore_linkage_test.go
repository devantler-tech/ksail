package kubescape_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	claircoreModulePath        = "github.com/quay/claircore"
	claircoreToolkitModulePath = "github.com/quay/claircore/toolkit"
)

type claircoreModuleAudit struct {
	versions      map[string]string
	inertPackages map[string]bool
}

// goListModule mirrors the Go command's exported JSON field names.
type goListModule struct {
	Path    string        `json:"Path"`    //nolint:tagliatelle // Go command output contract.
	Version string        `json:"Version"` //nolint:tagliatelle // Go command output contract.
	Replace *goListModule `json:"Replace"` //nolint:tagliatelle // Go command output contract.
}

// auditedClaircoreModules pins the independently selected Claircore modules
// and exact linked package set in every shipped KSail Go module. A toolkit-only
// upgrade or a package entering only one dependency graph must invalidate that
// module's reachability verdict without inheriting another module's audit.
func auditedClaircoreModules() map[string]claircoreModuleAudit {
	return map[string]claircoreModuleAudit{
		"root": {
			versions: map[string]string{
				claircoreModulePath:        "v1.5.53",
				claircoreToolkitModulePath: "v1.6.1",
			},
			inertPackages: map[string]bool{
				"github.com/quay/claircore":                   true,
				"github.com/quay/claircore/indexer":           true,
				"github.com/quay/claircore/internal/filterfs": true,
				"github.com/quay/claircore/osrelease":         true,
				"github.com/quay/claircore/pkg/cpe":           true,
				"github.com/quay/claircore/pkg/tarfs":         true,
				"github.com/quay/claircore/toolkit/log":       true,
				"github.com/quay/claircore/toolkit/types":     true,
				"github.com/quay/claircore/toolkit/types/cpe": true,
			},
		},
		"desktop": {
			versions: map[string]string{
				claircoreModulePath:        "v1.5.53",
				claircoreToolkitModulePath: "v1.6.1",
			},
			inertPackages: map[string]bool{
				"github.com/quay/claircore":                   true,
				"github.com/quay/claircore/indexer":           true,
				"github.com/quay/claircore/internal/filterfs": true,
				"github.com/quay/claircore/osrelease":         true,
				"github.com/quay/claircore/pkg/cpe":           true,
				"github.com/quay/claircore/pkg/tarfs":         true,
				"github.com/quay/claircore/toolkit/log":       true,
				"github.com/quay/claircore/toolkit/types":     true,
				"github.com/quay/claircore/toolkit/types/cpe": true,
			},
		},
	}
}

// TestClaircoreModuleDirsIncludesDesktop pins every Go module whose Claircore
// dependency graph must stay within the audited inert package set.
func TestClaircoreModuleDirsIncludesDesktop(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	got := claircoreModuleDirs(root)
	want := []string{root, filepath.Join(root, "desktop")}

	if len(got) != len(want) {
		t.Fatalf("expected %d guarded module directories, got %d: %v", len(want), len(got), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("module directory %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

// TestAuditedClaircoreVersionsIncludeToolkit prevents the independently
// versioned toolkit module from dropping out of the fail-closed audit.
func TestAuditedClaircoreVersionsIncludeToolkit(t *testing.T) {
	t.Parallel()

	wantModulePaths := []string{claircoreModulePath, claircoreToolkitModulePath}

	for _, moduleName := range []string{"root", "desktop"} {
		t.Run(moduleName, func(t *testing.T) {
			t.Parallel()

			audit, ok := auditedClaircoreModules()[moduleName]
			if !ok {
				t.Fatalf("module %q has no audited Claircore module versions", moduleName)
			}

			if len(audit.versions) != len(wantModulePaths) {
				t.Fatalf(
					"module %q must audit %d Claircore modules, got %d: %v",
					moduleName,
					len(wantModulePaths),
					len(audit.versions),
					audit.versions,
				)
			}

			for _, modulePath := range wantModulePaths {
				if audit.versions[modulePath] == "" {
					t.Errorf("module %q has no audited version for %q", moduleName, modulePath)
				}
			}
		})
	}
}

// TestClaircoreLinkedPackagesStayInert pins the reachability verdict of the
// Claircore manifest-URI SSRF (issue #6008): the advisory's sink is
// claircore's remote fetching code (libindex and its fetcher packages), which
// is NOT linked into ksail — only the inert packages above are. Every shipped
// module now runs a claircore release outside the advisory's affected range,
// so the sink is patched as well as unlinked; this guard keeps both properties
// true. Any new claircore package entering a module's dependency graph (for
// example via a kubescape bump), or a move away from an audited release, must
// fail this test so the verdict is re-established instead of silently trusted.
func TestClaircoreLinkedPackagesStayInert(t *testing.T) {
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

			audit, ok := auditedClaircoreModules()[name]
			if !ok {
				t.Fatalf("module %q has no Claircore audit", name)
			}

			assertClaircoreVersions(t, moduleDir, audit.versions)
			assertClaircorePackagesStayInert(t, moduleDir, audit.inertPackages)
		})
	}
}

// claircoreModuleDirs returns every Go module whose dependency graph ships as
// part of KSail and therefore needs an independently re-established SSRF
// reachability verdict after dependency changes.
func claircoreModuleDirs(root string) []string {
	return []string{root, filepath.Join(root, "desktop")}
}

// assertClaircoreVersions invalidates the reachability verdict whenever a
// shipped module moves away from either audited Claircore release, even if its
// package paths remain within the existing allow-list.
func assertClaircoreVersions(
	t *testing.T,
	moduleDir string,
	auditedVersions map[string]string,
) {
	t.Helper()

	for modulePath, auditedVersion := range auditedVersions {
		//nolint:gosec // modulePath is selected only from the fixed audited-version map above.
		cmd := exec.CommandContext(
			t.Context(),
			"go",
			"list",
			"-m",
			"-json",
			modulePath,
		)
		cmd.Dir = moduleDir

		out, err := cmd.Output()
		if err != nil {
			var stderr string

			exitErr := new(exec.ExitError)
			if errors.As(err, &exitErr) {
				stderr = string(exitErr.Stderr)
			}

			t.Fatalf(
				"read %q version from module %q: %v\n%s",
				modulePath,
				moduleDir,
				err,
				stderr,
			)
		}

		actual := goListModule{}

		decodeErr := json.Unmarshal(out, &actual)
		if decodeErr != nil {
			t.Fatalf("decode %q module metadata from %q: %v", modulePath, moduleDir, decodeErr)
		}

		validationFailure := validateClaircoreModuleVersion(modulePath, auditedVersion, actual)
		if validationFailure != "" {
			t.Fatalf("module %q: %s", moduleDir, validationFailure)
		}
	}
}

func validateClaircoreModuleVersion(
	modulePath string,
	auditedVersion string,
	actual goListModule,
) string {
	if actual.Replace != nil {
		return fmt.Sprintf(
			"%s %q is replaced by %s %q; re-establish the #6008 SSRF reachability verdict",
			modulePath,
			actual.Version,
			actual.Replace.Path,
			actual.Replace.Version,
		)
	}

	if actual.Path != modulePath {
		return fmt.Sprintf("requested %s but go list returned %s", modulePath, actual.Path)
	}

	if actual.Version != auditedVersion {
		return fmt.Sprintf(
			"uses %s %q; re-establish the #6008 SSRF reachability verdict before updating the audited version %q",
			modulePath,
			actual.Version,
			auditedVersion,
		)
	}

	return ""
}

// assertClaircorePackagesStayInert fails when a module links any Claircore
// package outside the audited set.
func assertClaircorePackagesStayInert(
	t *testing.T,
	moduleDir string,
	inertPackages map[string]bool,
) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "./...")
	cmd.Dir = moduleDir

	out, err := cmd.Output()
	if err != nil {
		var stderr string

		exitErr := new(exec.ExitError)
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}

		t.Fatalf("go list -deps ./... in module %q failed: %v\n%s", moduleDir, err, stderr)
	}

	var unexpected []string

	for pkg := range strings.FieldsSeq(string(out)) {
		isClaircore := pkg == "github.com/quay/claircore" ||
			strings.HasPrefix(pkg, "github.com/quay/claircore/")
		if isClaircore && !inertPackages[pkg] {
			unexpected = append(unexpected, pkg)
		}
	}

	if len(unexpected) > 0 {
		t.Fatalf(
			"module %q links claircore packages outside the audited inert set: %v — "+
				"re-establish the #6008 SSRF reachability verdict before extending "+
				"inertClaircorePackages",
			moduleDir,
			unexpected,
		)
	}
}

// moduleRoot walks up from the test's working directory to the directory
// holding the module's go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the test's working directory")
		}

		dir = parent
	}
}

// TestInertClaircorePackagesExcludesRemoteFetchingCode asserts the claircore
// packages that implement remote layer/manifest fetching — the sink behind
// the #6008 SSRF advisory — never enter the inert allow-list. A failing
// TestClaircoreLinkedPackagesStayInert must be resolved by re-establishing
// the reachability verdict, not by widening the list with a sink package.
func TestInertClaircorePackagesExcludesRemoteFetchingCode(t *testing.T) {
	t.Parallel()

	sensitive := []string{
		"github.com/quay/claircore/libindex",
		"github.com/quay/claircore/updater",
		"github.com/quay/claircore/pkg/distlock",
	}

	for moduleName, audit := range auditedClaircoreModules() {
		t.Run(moduleName, func(t *testing.T) {
			t.Parallel()

			for _, pkg := range sensitive {
				if audit.inertPackages[pkg] {
					t.Errorf(
						"expected %q to be absent from the inert allow-list; it performs remote fetching",
						pkg,
					)
				}
			}
		})
	}
}

// TestRootClaircoreAuditExcludesDesktopOnlyPackages prevents a package already
// audited for desktop from silently entering the root audit. Root may admit
// such a package only once root itself links it and the #6008 reachability
// verdict has been re-established against root's own dependency graph. Admitting
// one that root does not link is inheriting desktop's verdict, which the
// per-module audit exists to prevent.
func TestRootClaircoreAuditExcludesDesktopOnlyPackages(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not available on PATH")
	}

	rootAudit := auditedClaircoreModules()["root"]
	desktopAudit := auditedClaircoreModules()["desktop"]
	rootLinked := linkedClaircorePackages(t, moduleRoot(t))

	inherited := make([]string, 0, len(desktopAudit.inertPackages))

	for pkg := range desktopAudit.inertPackages {
		if rootAudit.inertPackages[pkg] && !rootLinked[pkg] {
			inherited = append(inherited, pkg)
		}
	}

	sort.Strings(inherited)

	for _, pkg := range inherited {
		t.Errorf(
			"root audit admits desktop-audited package %q that root does not link; "+
				"remove it rather than inheriting desktop's #6008 verdict",
			pkg,
		)
	}
}

// linkedClaircorePackages returns the Claircore packages a module's dependency
// graph actually links, so an audit can be checked against reality rather than
// against another module's audit.
func linkedClaircorePackages(t *testing.T, moduleDir string) map[string]bool {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "./...")
	cmd.Dir = moduleDir

	out, err := cmd.Output()
	if err != nil {
		var stderr string

		exitErr := new(exec.ExitError)
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}

		t.Fatalf("go list -deps ./... in module %q failed: %v\n%s", moduleDir, err, stderr)
	}

	linked := map[string]bool{}

	for pkg := range strings.FieldsSeq(string(out)) {
		isClaircore := pkg == "github.com/quay/claircore" ||
			strings.HasPrefix(pkg, "github.com/quay/claircore/")
		if isClaircore {
			linked[pkg] = true
		}
	}

	return linked
}

// TestValidateClaircoreModuleVersionRejectsReplacement prevents a fork or
// local replacement from inheriting the selected release's reachability
// verdict.
func TestValidateClaircoreModuleVersionRejectsReplacement(t *testing.T) {
	t.Parallel()

	actual := goListModule{
		Path:    claircoreModulePath,
		Version: "v1.5.35",
		Replace: &goListModule{Path: "example.com/claircore-fork", Version: "v1.5.35"},
	}

	validationFailure := validateClaircoreModuleVersion(claircoreModulePath, "v1.5.35", actual)
	if validationFailure == "" {
		t.Fatal("expected a replacement module to invalidate the Claircore audit")
	}
}

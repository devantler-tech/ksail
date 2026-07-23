package kubescape_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	claircoreModulePath        = "github.com/quay/claircore"
	claircoreToolkitModulePath = "github.com/quay/claircore/toolkit"
)

// auditedClaircoreVersions pins the independently selected Claircore modules
// in every shipped KSail Go module. A toolkit-only upgrade can change linked
// code without changing the parent Claircore module, so both versions are part
// of the reachability verdict.
func auditedClaircoreVersions() map[string]map[string]string {
	return map[string]map[string]string{
		"root": {
			claircoreModulePath:        "v1.5.35",
			claircoreToolkitModulePath: "v1.2.4",
		},
		"desktop": {
			claircoreModulePath:        "v1.5.53",
			claircoreToolkitModulePath: "v1.6.1",
		},
	}
}

// inertClaircorePackages is the full set of claircore packages the ksail
// binary may link. They are parsing/type/local-filesystem code only — none
// performs the remote layer/manifest fetching behind the Claircore
// manifest-URI SSRF advisory (issue #6008, Dependabot alerts #165/#166).
func inertClaircorePackages() map[string]bool {
	return map[string]bool{
		"github.com/quay/claircore":                   true,
		"github.com/quay/claircore/indexer":           true,
		"github.com/quay/claircore/internal/filterfs": true,
		"github.com/quay/claircore/osrelease":         true,
		"github.com/quay/claircore/pkg/cpe":           true,
		"github.com/quay/claircore/pkg/tarfs":         true,
		"github.com/quay/claircore/toolkit/log":       true,
		"github.com/quay/claircore/toolkit/types":     true,
		"github.com/quay/claircore/toolkit/types/cpe": true,
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

			auditedVersions, ok := auditedClaircoreVersions()[moduleName]
			if !ok {
				t.Fatalf("module %q has no audited Claircore module versions", moduleName)
			}

			if len(auditedVersions) != len(wantModulePaths) {
				t.Fatalf(
					"module %q must audit %d Claircore modules, got %d: %v",
					moduleName,
					len(wantModulePaths),
					len(auditedVersions),
					auditedVersions,
				)
			}

			for _, modulePath := range wantModulePaths {
				if auditedVersions[modulePath] == "" {
					t.Errorf("module %q has no audited version for %q", moduleName, modulePath)
				}
			}
		})
	}
}

// TestClaircoreLinkedPackagesStayInert pins the reachability verdict of the
// Claircore manifest-URI SSRF (issue #6008): the advisory's sink is
// claircore's remote fetching code (libindex and its fetcher packages), which
// is NOT linked into ksail — only the inert packages above are. Until a
// patched claircore release ships and is adopted, any new claircore package
// entering the dependency graph (for example via a kubescape bump) must fail
// this test so the verdict is re-established instead of silently trusted.
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

			versions, ok := auditedClaircoreVersions()[name]
			if !ok {
				t.Fatalf("module %q has no audited Claircore module versions", name)
			}

			assertClaircoreVersions(t, moduleDir, versions)
			assertClaircorePackagesStayInert(t, moduleDir)
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
			"-f",
			"{{.Version}}",
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

		if got := strings.TrimSpace(string(out)); got != auditedVersion {
			t.Fatalf(
				"module %q uses %s %q; re-establish the #6008 SSRF reachability verdict "+
					"before updating the audited version %q",
				moduleDir,
				modulePath,
				got,
				auditedVersion,
			)
		}
	}
}

// assertClaircorePackagesStayInert fails when a module links any Claircore
// package outside the audited set.
func assertClaircorePackagesStayInert(t *testing.T, moduleDir string) {
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
		if isClaircore && !inertClaircorePackages()[pkg] {
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

	inert := inertClaircorePackages()

	for _, pkg := range sensitive {
		if inert[pkg] {
			t.Errorf(
				"expected %q to be absent from the inert allow-list; it performs remote fetching",
				pkg,
			)
		}
	}
}

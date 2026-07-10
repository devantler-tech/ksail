package kubescape_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// inertClaircorePackages is the full set of claircore packages the ksail
// binary may link. They are parsing/type/local-filesystem code only — none
// performs the remote layer/manifest fetching behind the Claircore
// manifest-URI SSRF advisory (issue #6008, Dependabot alerts #165/#166).
func inertClaircorePackages() map[string]bool {
	return map[string]bool{
		"github.com/quay/claircore":                   true,
		"github.com/quay/claircore/indexer":           true,
		"github.com/quay/claircore/osrelease":         true,
		"github.com/quay/claircore/pkg/cpe":           true,
		"github.com/quay/claircore/pkg/tarfs":         true,
		"github.com/quay/claircore/toolkit/types/cpe": true,
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

	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "./...")
	cmd.Dir = moduleRoot(t)

	out, err := cmd.Output()
	if err != nil {
		var stderr string

		exitErr := new(exec.ExitError)
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}

		t.Fatalf("go list -deps ./... failed: %v\n%s", err, stderr)
	}

	var unexpected []string

	for pkg := range strings.FieldsSeq(string(out)) {
		if strings.HasPrefix(pkg, "github.com/quay/claircore") && !inertClaircorePackages()[pkg] {
			unexpected = append(unexpected, pkg)
		}
	}

	if len(unexpected) > 0 {
		t.Fatalf(
			"claircore packages outside the audited inert set are now linked: %v — "+
				"re-establish the #6008 SSRF reachability verdict before extending "+
				"inertClaircorePackages",
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

// TestInertClaircorePackagesContents pins the exact allow-list backing the
// #6008 SSRF reachability verdict, so any addition or removal is a
// deliberate, reviewable change instead of an accidental one.
func TestInertClaircorePackagesContents(t *testing.T) {
	t.Parallel()

	want := []string{
		"github.com/quay/claircore",
		"github.com/quay/claircore/indexer",
		"github.com/quay/claircore/osrelease",
		"github.com/quay/claircore/pkg/cpe",
		"github.com/quay/claircore/pkg/tarfs",
		"github.com/quay/claircore/toolkit/types/cpe",
	}

	got := inertClaircorePackages()

	if len(got) != len(want) {
		t.Fatalf("expected %d inert packages, got %d: %v", len(want), len(got), got)
	}

	for _, pkg := range want {
		if !got[pkg] {
			t.Errorf("expected %q to be marked inert, got %v", pkg, got)
		}
	}
}

// TestInertClaircorePackagesReturnsIndependentMap asserts each call returns
// its own map instance, so a caller mutating its result cannot leak state
// into a subsequent call.
func TestInertClaircorePackagesReturnsIndependentMap(t *testing.T) {
	t.Parallel()

	const sentinel = "github.com/quay/claircore/libindex"

	first := inertClaircorePackages()
	first[sentinel] = true

	second := inertClaircorePackages()
	if second[sentinel] {
		t.Fatal(
			"mutating one call's result affected a subsequent call; " +
				"inertClaircorePackages must return a fresh map each time",
		)
	}
}

// TestInertClaircorePackagesExcludesRemoteFetchingCode asserts the claircore
// packages that implement remote layer/manifest fetching — the sink behind
// the #6008 SSRF advisory — are never present in the allow-list. If one of
// these ever needs to be linked, the reachability verdict must be
// re-established rather than silently widening this list.
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
			t.Errorf("expected %q to be absent from the inert allow-list; it performs remote fetching", pkg)
		}
	}
}

// TestModuleRootFindsGoMod asserts moduleRoot locates the directory holding
// this module's go.mod when invoked from the test's own working directory.
func TestModuleRootFindsGoMod(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod at reported module root %q: %v", root, err)
	}

	const wantModule = "module github.com/devantler-tech/ksail/v7"
	if !strings.Contains(string(data), wantModule) {
		t.Fatalf("go.mod at %q does not declare %q:\n%s", root, wantModule, data)
	}
}

// TestModuleRootFromNestedSubdirectory asserts moduleRoot walks up through
// multiple intermediate directories — not just the immediate parent — to
// find go.mod. It cannot call t.Parallel because t.Chdir affects the whole
// process's working directory.
func TestModuleRootFromNestedSubdirectory(t *testing.T) {
	want := moduleRoot(t)

	nested, err := os.MkdirTemp(".", "moduleroot-nested-*")
	if err != nil {
		t.Fatalf("create nested temp directory: %v", err)
	}

	t.Cleanup(func() {
		if err := os.RemoveAll(nested); err != nil {
			t.Errorf("remove nested temp directory: %v", err)
		}
	})

	deeper := filepath.Join(nested, "a", "b", "c")
	if err := os.MkdirAll(deeper, 0o755); err != nil {
		t.Fatalf("create deeply nested temp directory: %v", err)
	}

	t.Chdir(deeper)

	if got := moduleRoot(t); got != want {
		t.Fatalf("moduleRoot from nested directory = %q, want %q", got, want)
	}
}

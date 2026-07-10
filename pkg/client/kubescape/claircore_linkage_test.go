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

// TestInertClaircorePackagesContents pins the exact audited set of claircore
// packages considered inert for the #6008 SSRF reachability verdict. Any
// change to this set (an addition, a removal, or a flipped value) should be
// a deliberate, reviewed decision, so this test fails on any drift.
func TestInertClaircorePackagesContents(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"github.com/quay/claircore":                   true,
		"github.com/quay/claircore/indexer":           true,
		"github.com/quay/claircore/osrelease":         true,
		"github.com/quay/claircore/pkg/cpe":           true,
		"github.com/quay/claircore/pkg/tarfs":         true,
		"github.com/quay/claircore/toolkit/types/cpe": true,
	}

	got := inertClaircorePackages()

	if len(got) != len(want) {
		t.Fatalf("expected %d inert packages, got %d: %v", len(want), len(got), got)
	}

	for pkg, wantOK := range want {
		gotOK, ok := got[pkg]
		if !ok {
			t.Errorf("expected package %q to be present in the inert set", pkg)

			continue
		}

		if gotOK != wantOK {
			t.Errorf("package %q: expected value %v, got %v", pkg, wantOK, gotOK)
		}
	}
}

// TestInertClaircorePackagesFreshMap asserts each call returns an
// independent map, so mutating one call's result cannot leak into another
// call or into the reachability check itself.
func TestInertClaircorePackagesFreshMap(t *testing.T) {
	t.Parallel()

	const mutatedKey = "github.com/quay/claircore/mutated"

	first := inertClaircorePackages()
	first[mutatedKey] = true

	second := inertClaircorePackages()
	if _, ok := second[mutatedKey]; ok {
		t.Fatalf(
			"expected a fresh map from inertClaircorePackages, got a shared map containing %q",
			mutatedKey,
		)
	}
}

// TestModuleRootFindsGoModInCurrentDirectory asserts moduleRoot returns the
// starting directory immediately when it already holds go.mod, without
// walking up to any parent.
//
// This test calls t.Chdir, so it (and its ancestors) must not run in
// parallel; see the paralleltest exclusion for this file in .golangci.yml.
func TestModuleRootFindsGoModInCurrentDirectory(t *testing.T) {
	dir := t.TempDir()

	writeGoMod(t, dir)
	t.Chdir(dir)

	assertModuleRootResolvesTo(t, dir)
}

// TestModuleRootWalksUpMultipleLevels asserts moduleRoot walks up parent
// directories until it finds the go.mod that marks the module root, rather
// than stopping at (or failing on) the first directory it inspects.
//
// This test calls t.Chdir, so it (and its ancestors) must not run in
// parallel; see the paralleltest exclusion for this file in .golangci.yml.
func TestModuleRootWalksUpMultipleLevels(t *testing.T) {
	root := t.TempDir()

	writeGoMod(t, root)

	nested := filepath.Join(root, "a", "b", "c")
	err := os.MkdirAll(nested, 0o750)
	if err != nil {
		t.Fatalf("create nested directories: %v", err)
	}

	t.Chdir(nested)

	assertModuleRootResolvesTo(t, root)
}

// TestModuleRootFailsWhenNoGoModFound asserts moduleRoot fails the test
// (rather than looping forever or returning a bogus directory) when no
// go.mod exists anywhere above the working directory.
//
// This test calls t.Chdir, so it (and its ancestors) must not run in
// parallel; see the paralleltest exclusion for this file in .golangci.yml.
func TestModuleRootFailsWhenNoGoModFound(t *testing.T) {
	dir := t.TempDir()

	t.Chdir(dir)

	ok := t.Run("moduleRootWithoutGoMod", func(t *testing.T) {
		moduleRoot(t)
	})

	if ok {
		t.Fatal(
			"expected moduleRoot to fail the test when no go.mod is found above the working directory",
		)
	}
}

// writeGoMod creates a minimal go.mod file directly inside dir.
func writeGoMod(t *testing.T, dir string) {
	t.Helper()

	err := os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/moduleroottest\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

// assertModuleRootResolvesTo asserts moduleRoot(t) returns a directory
// referring to the same location as want, resolving symlinks on both sides
// so platforms where the temp directory is itself a symlink (for example
// macOS's /tmp) compare equal.
func assertModuleRootResolvesTo(t *testing.T, want string) {
	t.Helper()

	got := moduleRoot(t)

	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("resolve symlinks for returned dir %q: %v", got, err)
	}

	wantResolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve symlinks for expected dir %q: %v", want, err)
	}

	if gotResolved != wantResolved {
		t.Fatalf("expected moduleRoot to return %q, got %q", wantResolved, gotResolved)
	}
}

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
		isClaircore := pkg == "github.com/quay/claircore" ||
			strings.HasPrefix(pkg, "github.com/quay/claircore/")
		if isClaircore && !inertClaircorePackages()[pkg] {
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

package licensepolicy_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/licensepolicy"
	"golang.org/x/mod/sumdb/dirhash"
)

func TestOrdinaryLicenseDecisions(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		raw  []string
		pass bool
	}{
		{"allowed", []string{"MIT"}, true},
		{"additional-obligations", []string{"MIT", "OFL-1.1"}, true},
		{"denied", []string{"GPL-2.0"}, false},
		{"mixed-denied", []string{"MIT", "GPL-2.0"}, false},
		{"unclassified", nil, false},
		{"unknown", []string{"Unknown"}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			policy, root, finding := fixture(t)
			finding.Detected = testCase.raw
			before := append([]string(nil), finding.Detected...)

			_, err := licensepolicy.Evaluate(policy, root, finding)
			if (err == nil) != testCase.pass {
				t.Fatalf("pass=%v, want %v: %v", err == nil, testCase.pass, err)
			}

			if !reflect.DeepEqual(before, finding.Detected) {
				t.Fatal("raw classifier findings were altered")
			}
		})
	}
}

func TestExceptionRequiresExactAuthenticatedSource(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"accepted", "version", "checksum", "changed-source", "changed-license",
		"replacement", "prefix-collision", "different-package", "different-module",
		"different-detection", "unsafe-evidence", "symlink",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			policy, root, finding := fixture(t)
			finding.Detected = []string{"Unknown"}
			policy.Exceptions = []licensepolicy.Exception{{
				Packages: []string{finding.Package}, Module: finding.Module.Path,
				Version: finding.Module.Version, Sum: finding.Module.Sum,
				Evidence: "LICENSE", SHA256: digest("declared Apache terms"),
				Detected: []string{"Unknown"}, Effective: []string{"Apache-2.0"},
			}}

			changeExceptionFixture(t, name, root, &policy, &finding)

			verdict, err := licensepolicy.Evaluate(policy, root, finding)
			if (err == nil) != (name == "accepted") {
				t.Fatalf("%s: verdict=%v error=%v", name, verdict, err)
			}

			if err == nil && !reflect.DeepEqual(verdict, []string{"Apache-2.0"}) {
				t.Fatalf("unexpected effective license: %v", verdict)
			}
		})
	}
}

func TestFirstPartyOwnershipDoesNotApproveThirdPartyUnknowns(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"owned", "code-and-directory", "lookalike-module", "foreign-root", "changed-license", "nested-terms",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			policy, root, finding := fixture(t)
			finding.Module = licensepolicy.Module{
				Path: policy.FirstPartyModule,
				Dir:  root,
				Main: true,
			}
			finding.Package = policy.FirstPartyModule + "/pkg/owned"

			finding.PackageDir = filepath.Join(root, "pkg", "owned")

			err := os.MkdirAll(finding.PackageDir, 0o700)
			if err != nil {
				t.Fatal(err)
			}

			finding.Detected = []string{"Unknown"}

			changeOwnedFixture(t, name, root, &finding)

			_, err = licensepolicy.Evaluate(policy, root, finding)
			if (err == nil) != (name == "owned" || name == "code-and-directory") {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
}

func TestFirstPartyCannotInheritAcrossUnexaminedCandidateTerms(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"README.md", "UNLICENSE", "UNLICENCE", "LICENSES", "licenceTerms", "NOTICEextra", "COPYINGadditional",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			policy, root, finding := fixture(t)
			finding.Module = licensepolicy.Module{
				Path: policy.FirstPartyModule,
				Dir:  root,
				Main: true,
			}
			finding.Package = policy.FirstPartyModule + "/nested"
			finding.PackageDir = filepath.Join(root, "nested")
			finding.Detected = []string{"Unknown"}

			err := os.Mkdir(finding.PackageDir, 0o700)
			if err != nil {
				t.Fatal(err)
			}

			write(t, filepath.Join(finding.PackageDir, name), "unrecognised nested terms")

			_, err = licensepolicy.Evaluate(policy, root, finding)
			if err == nil {
				t.Fatal("unexamined nested terms inherited root approval")
			}
		})
	}
}

func TestLicenseChoiceDoesNotPermitGPLGenerally(t *testing.T) {
	t.Parallel()
	policy, root, finding := fixture(t)
	finding.Detected = []string{"Apache-2.0", "GPL-2.0"}
	finding.LicenseFile = filepath.Join(finding.Module.Dir, "LICENSE")
	policy.Exceptions = []licensepolicy.Exception{{
		Packages: []string{finding.Package}, Module: finding.Module.Path,
		Version: finding.Module.Version, Sum: finding.Module.Sum,
		Evidence: "LICENSE", ReportedFile: "LICENSE", SHA256: digest("declared Apache terms"),
		Detected: []string{"GPL-2.0", "Apache-2.0"}, Effective: []string{"Apache-2.0"},
	}}

	got, err := licensepolicy.Evaluate(policy, root, finding)
	if err != nil || !reflect.DeepEqual(got, []string{"Apache-2.0"}) {
		t.Fatalf("choice: %v %v", got, err)
	}

	finding.Package += "/another"

	_, err = licensepolicy.Evaluate(policy, root, finding)
	if err == nil {
		t.Fatal("license choice escaped exact package scope")
	}
}

func TestFirstPartyParentAliasUsesTheSamePhysicalLicense(t *testing.T) {
	t.Parallel()
	policy, root, finding := fixture(t)
	finding.PackageDir = filepath.Join(root, "pkg")

	err := os.Mkdir(finding.PackageDir, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	alias := filepath.Join(t.TempDir(), "alias")

	err = os.Symlink(filepath.Dir(root), alias)
	if err != nil {
		t.Fatal(err)
	}

	aliasedRoot := filepath.Join(alias, filepath.Base(root))
	finding.Module = licensepolicy.Module{
		Path: policy.FirstPartyModule,
		Dir:  aliasedRoot,
		Main: true,
	}
	finding.Package = policy.FirstPartyModule + "/pkg"
	finding.Detected = []string{"Unknown"}

	_, err = licensepolicy.Evaluate(policy, aliasedRoot, finding)
	if err != nil {
		t.Fatalf("same physical first-party source was rejected: %v", err)
	}
}

func TestInvalidOwnershipNeverProducesAnApproval(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"missing-source", "relative-source", "relative-package", "package-escape", "license-escape", "package-symlink",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			policy, root, finding := fixture(t)
			finding.Detected = []string{"MIT"}

			switch name {
			case "missing-source":
				finding.Module.Dir = filepath.Join(root, "absent")
			case "relative-source":
				finding.Module.Dir = "relative"
			case "relative-package":
				finding.PackageDir = "relative"
			case "package-escape":
				finding.PackageDir = root
			case "license-escape":
				finding.LicenseFile = filepath.Join(root, "LICENSE")
			case "package-symlink":
				linked := filepath.Join(finding.Module.Dir, "linked")

				err := os.Symlink(root, linked)
				if err != nil {
					t.Fatal(err)
				}

				finding.PackageDir = linked
			}

			_, err := licensepolicy.Evaluate(policy, root, finding)
			if err == nil {
				t.Fatal("unexamined or foreign source approved")
			}
		})
	}
}

func changeExceptionFixture(
	t *testing.T,
	name, root string,
	policy *licensepolicy.Policy,
	finding *licensepolicy.Finding,
) {
	t.Helper()

	changes := map[string]func(){
		"version":             func() { finding.Module.Version = "v1.0.1" },
		"checksum":            func() { finding.Module.Sum = "h1:changed" },
		"replacement":         func() { finding.Module.Replaced = true },
		"prefix-collision":    func() { finding.Package += "-lookalike" },
		"different-package":   func() { finding.Package += "/new" },
		"different-module":    func() { finding.Module.Path += "-lookalike" },
		"different-detection": func() { finding.Detected = []string{"GPL-2.0"} },
		"unsafe-evidence":     func() { policy.Exceptions[0].Evidence = "../LICENSE" },
	}
	if change := changes[name]; change != nil {
		change()

		return
	}

	switch name {
	case "changed-source":
		write(t, filepath.Join(finding.Module.Dir, "code.go"), "package changed")
	case "changed-license":
		write(t, filepath.Join(finding.Module.Dir, "LICENSE"), "different terms")
	case "symlink":
		err := os.Symlink(
			filepath.Join(root, "LICENSE"),
			filepath.Join(finding.Module.Dir, "linked"),
		)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func changeOwnedFixture(t *testing.T, name, root string, finding *licensepolicy.Finding) {
	t.Helper()

	switch name {
	case "code-and-directory":
		write(t, filepath.Join(finding.PackageDir, "license_policy_test.go"), "package owned")

		err := os.Mkdir(filepath.Join(root, "pkg", "licensepolicy"), 0o700)
		if err != nil {
			t.Fatal(err)
		}
	case "lookalike-module":
		finding.Module.Path += "-lookalike"
	case "foreign-root":
		finding.Module.Dir = t.TempDir()
	case "changed-license":
		write(t, filepath.Join(root, "LICENSE"), "different owner terms")
	case "nested-terms":
		write(t, filepath.Join(finding.PackageDir, "LICENSE"), "third party terms")
	}
}

func fixture(t *testing.T) (licensepolicy.Policy, string, licensepolicy.Finding) {
	t.Helper()
	root, source := t.TempDir(), t.TempDir()
	write(t, filepath.Join(root, "LICENSE"), "owned terms")
	write(t, filepath.Join(source, "LICENSE"), "declared Apache terms")
	write(t, filepath.Join(source, "go.mod"), "module example.test/dependency\n")
	write(t, filepath.Join(source, "code.go"), "package dependency\n")

	sum, err := dirhash.HashDir(source, "example.test/dependency@v1.0.0", dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}

	return licensepolicy.Policy{
		Allowed: []string{"MIT", "Apache-2.0", "OFL-1.1"}, FirstPartyModule: "example.test/product",
		FirstPartySHA256: digest("owned terms"), FirstPartyLicense: "LicenseRef-Owned",
	}, root, licensepolicy.Finding{
		Package:    "example.test/dependency",
		PackageDir: source,
		Module: licensepolicy.Module{
			Path:    "example.test/dependency",
			Version: "v1.0.0",
			Sum:     sum,
			Dir:     source,
		},
	}
}

func write(t *testing.T, path, contents string) {
	t.Helper()

	err := os.WriteFile(path, []byte(contents), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func digest(contents string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(contents))) }

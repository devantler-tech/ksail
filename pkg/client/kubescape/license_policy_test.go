package kubescape_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/licensepolicy"
	"github.com/google/go-licenses/v2/licenses"
)

type licenseTarget struct {
	name, entry, cgo, tags string
	platform               platform
}

type strictLicenseClassifier struct {
	delegate licenses.Classifier
	mutex    sync.Mutex
	failed   bool
	cache    map[string][]licenses.License
}

func (classifier *strictLicenseClassifier) Identify(path string) ([]licenses.License, error) {
	// Libraries shares this classifier across workers. Its delegate has mutable
	// diff state, and the same immutable source evidence is used by every target.
	classifier.mutex.Lock()
	defer classifier.mutex.Unlock()

	if identified, exists := classifier.cache[path]; exists {
		return slices.Clone(identified), nil
	}

	identified, err := classifier.delegate.Identify(path)
	if err != nil {
		classifier.failed = true

		return nil, fmt.Errorf("classify license evidence: %w", err)
	}

	if classifier.cache == nil {
		classifier.cache = make(map[string][]licenses.License)
	}

	classifier.cache[path] = slices.Clone(identified)

	return slices.Clone(identified), nil
}

func (classifier *strictLicenseClassifier) hadFailure() bool {
	classifier.mutex.Lock()
	defer classifier.mutex.Unlock()

	return classifier.failed
}

type licenseReportRow struct {
	Package     string               `json:"package"`
	Module      string               `json:"module"`
	Version     string               `json:"version"`
	Checksum    string               `json:"checksum"`
	Replacement *licenseReportSource `json:"replacement,omitempty"`
	Raw         []string             `json:"raw"`
	Effective   []string             `json:"effective"`
}

type licenseReportSource struct {
	Kind     string `json:"kind"`
	Module   string `json:"module,omitempty"`
	Version  string `json:"version,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

// This test deliberately runs sequentially: packages.Load consumes Go's target
// environment. Its scanner import is test-only and never enters a shipped CLI.
func TestReleasedGoDependencyLicenses(t *testing.T) {
	root := moduleRoot(t)
	t.Chdir(root)

	for key, value := range map[string]string{
		"GOROOT": activeLicenseGoRoot(t, root), "GOFLAGS": "-mod=readonly", "GOWORK": "off",
		"GOENV": "off", "GOEXPERIMENT": "", "GOPACKAGESDRIVER": "off",
	} {
		t.Setenv(key, value)
	}

	base, err := licenses.NewClassifier()
	if err != nil {
		t.Fatal("cannot construct pinned license classifier")
	}

	classifier := &strictLicenseClassifier{delegate: base}

	policy := reviewedGoLicensePolicy(t, root)
	for _, target := range licenseTargets(t, root) {
		t.Run(target.name, func(t *testing.T) {
			t.Setenv("GOOS", target.platform.goos)
			t.Setenv("GOARCH", target.platform.goarch)
			t.Setenv("CGO_ENABLED", target.cgo)

			flags := "-mod=readonly"
			if target.tags != "" {
				flags += " -tags=" + target.tags
			}

			t.Setenv("GOFLAGS", flags)
			packages := listDependencyEntry(
				t,
				root,
				target.platform,
				target.cgo,
				target.tags,
				target.entry,
			)

			libraries, err := licenses.Libraries(t.Context(), classifier, false, nil, target.entry)
			if err != nil || classifier.hadFailure() {
				t.Fatal("license inventory or source-file classification failed")
			}

			if len(libraries) == 0 {
				t.Fatal("license inventory examined no libraries")
			}

			rows := evaluateLicenseInventory(t, root, policy, packages, libraries)
			writeLicenseReport(t, target.name, rows)
		})
	}
}

func licenseTargets(t *testing.T, root string) []licenseTarget {
	t.Helper()

	var result []licenseTarget
	for _, target := range goreleaserTargets(t, filepath.Join(root, ".goreleaser.yaml")) {
		result = append(
			result,
			licenseTarget{
				"cli-" + target.GOOS + "-" + target.GOARCH,
				".",
				"0",
				"",
				platform{target.GOOS, target.GOARCH},
			},
		)
	}

	if len(result) == 0 {
		t.Fatal("no CLI release targets were examined")
	}

	desktop := append(
		goreleaserTargets(t, filepath.Join(root, ".goreleaser.desktop.yaml")),
		desktopWorkflowTargets(t, filepath.Join(root, ".github/workflows/cd.yaml"))...)
	if len(desktop) == 0 {
		t.Fatal("no desktop release targets were examined")
	}

	seen := map[releaseTarget]bool{}
	for _, target := range desktop {
		if seen[target] {
			continue
		}

		seen[target] = true
		result = append(
			result,
			licenseTarget{
				"desktop-" + target.GOOS + "-" + target.GOARCH,
				"./desktop",
				"1",
				"desktop",
				platform{target.GOOS, target.GOARCH},
			},
		)
	}

	return result
}

func evaluateLicenseInventory(
	t *testing.T,
	root string,
	policy licensepolicy.Policy,
	packages []goListPackage,
	libraries []*licenses.Library,
) []licenseReportRow {
	t.Helper()

	expected, err := completeLicenseMembership(packages, libraries)
	if err != nil {
		t.Fatal(err)
	}

	var rows []licenseReportRow

	for _, library := range libraries {
		raw := make([]string, 0, len(library.Licenses))
		for _, identified := range library.Licenses {
			raw = append(raw, identified.Name)
		}

		if len(raw) == 0 {
			raw = []string{"Unknown"}
		}

		for _, name := range library.Packages {
			pkg := expected[name]
			finding := selectedLicenseFinding(pkg, name, library.LicenseFile, raw)
			effective, err := licensepolicy.Evaluate(policy, root, finding)

			rows = append(rows, packageLicenseReport(pkg, name, raw, effective))
			if err != nil {
				t.Errorf("unapproved dependency license: %s (%v)", name, raw)
			}
		}
	}

	slices.SortFunc(rows, func(a, b licenseReportRow) int {
		if a.Package < b.Package {
			return -1
		}

		if a.Package > b.Package {
			return 1
		}

		return 0
	})

	return rows
}

func packageLicenseReport(
	pkg goListPackage,
	name string,
	raw, effective []string,
) licenseReportRow {
	return licenseReportRow{
		Package:     name,
		Module:      pkg.Module.Path,
		Version:     pkg.Module.Version,
		Checksum:    pkg.Module.Sum,
		Replacement: licenseReplacementSource(pkg.Module),
		Raw:         slices.Clone(raw),
		Effective:   effective,
	}
}

func licenseReplacementSource(module *goListModule) *licenseReportSource {
	if module.Replace == nil {
		return nil
	}

	replacement := module.Replace
	if replacement.Version == "" {
		return &licenseReportSource{Kind: "local"}
	}

	return &licenseReportSource{
		Kind:     "module",
		Module:   replacement.Path,
		Version:  replacement.Version,
		Checksum: replacement.Sum,
	}
}

func selectedLicenseFinding(
	pkg goListPackage,
	name, licenseFile string,
	raw []string,
) licensepolicy.Finding {
	directory := pkg.Module.Dir
	if pkg.Module.Replace != nil {
		directory = pkg.Module.Replace.Dir
	}

	return licensepolicy.Finding{
		Package:     name,
		PackageDir:  pkg.Dir,
		LicenseFile: licenseFile,
		Detected:    slices.Clone(raw),
		Module: licensepolicy.Module{
			Path:     pkg.Module.Path,
			Version:  pkg.Module.Version,
			Sum:      pkg.Module.Sum,
			Dir:      directory,
			Main:     pkg.Module.Main,
			Replaced: pkg.Module.Replace != nil,
		},
	}
}

const (
	maxLicensePolicyBytes = 1 << 20
	licenseReportFolder   = "ksail-go-licenses"
	goLicenseScope        = "Selected Go source package graph; " +
		"native libraries and embedded non-Go contents require separate evidence"
)

var errLicenseInventory = errors.New("license inventory is incomplete or inconsistent")

func writeLicenseReport(t *testing.T, target string, rows []licenseReportRow) {
	t.Helper()

	if !filepath.IsLocal(target) || filepath.Base(target) != target {
		t.Fatal("invalid license report target")
	}

	directory := licenseReportRoot(t)

	contents, err := json.MarshalIndent(struct {
		Scope    string             `json:"scope"`
		Target   string             `json:"target"`
		Findings []licenseReportRow `json:"findings"`
	}{goLicenseScope, target, rows}, "", "  ")
	if err != nil {
		t.Fatal("cannot encode complete license report")
	}

	file, err := directory.OpenFile(target+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("cannot create unique license report")
	}

	t.Cleanup(func() {
		closeErr := file.Close()
		if closeErr != nil {
			t.Error("cannot close license report")
		}
	})

	_, err = file.Write(contents)
	if err != nil {
		t.Fatal("cannot preserve complete license report")
	}

	t.Logf("%s: preserved %d raw package findings and their dispositions", target, len(rows))
}

func licenseReportRoot(t *testing.T) *os.Root {
	t.Helper()

	configured := os.Getenv("KSAIL_GO_LICENSE_REPORT_DIR")
	if configured == "" {
		return licenseDirectoryRoot(t, t.TempDir())
	}

	temporary := os.Getenv("RUNNER_TEMP")
	if !validLicenseReportPath(configured, temporary) {
		t.Fatal("license report path must be the fixed hosted-runner output")
	}

	runner := licenseDirectoryRoot(t, temporary)

	err := runner.Mkdir(licenseReportFolder, 0o700)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		t.Fatal("cannot create license report directory")
	}

	return licenseDirectoryRoot(t, configured)
}

func validLicenseReportPath(configured, temporary string) bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" &&
		os.Getenv("RUNNER_ENVIRONMENT") == "github-hosted" &&
		filepath.IsAbs(temporary) &&
		configured == filepath.Join(temporary, licenseReportFolder)
}

func licenseDirectoryRoot(t *testing.T, directory string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal("cannot open scoped license directory")
	}

	t.Cleanup(func() {
		closeErr := root.Close()
		if closeErr != nil {
			t.Error("cannot close scoped license directory")
		}
	})

	return root
}

func reviewedGoLicensePolicy(t *testing.T, root string) licensepolicy.Policy {
	t.Helper()
	directory := licenseDirectoryRoot(t, root)

	file, err := directory.Open(".github/go-license-policy.json")
	if err != nil {
		t.Fatal("cannot open license policy")
	}

	t.Cleanup(func() {
		closeErr := file.Close()
		if closeErr != nil {
			t.Error("cannot close license policy")
		}
	})

	info, err := file.Stat()
	if err != nil || info.Size() == 0 || info.Size() > maxLicensePolicyBytes {
		t.Fatal("cannot read bounded license policy")
	}

	decoder := json.NewDecoder(io.LimitReader(file, maxLicensePolicyBytes+1))
	decoder.DisallowUnknownFields()

	var policy licensepolicy.Policy

	err = decoder.Decode(&policy)
	if err != nil {
		t.Fatal("invalid license policy")
	}

	err = decoder.Decode(new(any))
	if !errors.Is(err, io.EOF) {
		t.Fatal("license policy contains trailing data")
	}

	if incompleteLicensePolicy(policy) {
		t.Fatal("license policy is incomplete")
	}

	return policy
}

func incompleteLicensePolicy(policy licensepolicy.Policy) bool {
	return len(policy.Allowed) == 0 || policy.FirstPartyModule == "" || len(policy.Exceptions) == 0
}

func completeLicenseMembership(
	packages []goListPackage, libraries []*licenses.Library,
) (map[string]goListPackage, error) {
	expected, err := licenseOwnership(packages)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}

	for _, library := range libraries {
		if !validLicenseLibrary(library) {
			return nil, fmt.Errorf(
				"%w: empty library or license without source",
				errLicenseInventory,
			)
		}

		for _, name := range library.Packages {
			if _, found := expected[name]; !found || seen[name] {
				return nil, fmt.Errorf("%w: unexpected or repeated package", errLicenseInventory)
			}

			seen[name] = true
		}
	}

	if len(seen) != len(expected) {
		return nil, errLicenseInventory
	}

	return expected, nil
}

func licenseOwnership(packages []goListPackage) (map[string]goListPackage, error) {
	result := map[string]goListPackage{}

	for _, pkg := range packages {
		if pkg.Standard {
			continue
		}

		if pkg.ImportPath == "" || pkg.Module == nil || pkg.Dir == "" {
			return nil, fmt.Errorf("%w: missing source ownership", errLicenseInventory)
		}

		if _, duplicate := result[pkg.ImportPath]; duplicate {
			return nil, fmt.Errorf("%w: repeated dependency package", errLicenseInventory)
		}

		result[pkg.ImportPath] = pkg
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("%w: no nonstandard packages", errLicenseInventory)
	}

	return result, nil
}

func validLicenseLibrary(library *licenses.Library) bool {
	return library != nil && len(library.Packages) > 0 &&
		(len(library.Licenses) == 0 || library.LicenseFile != "")
}

func activeLicenseGoRoot(t *testing.T, root string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "go", "env", "GOROOT")
	command.Dir = root
	command.Env = auditGoEnv("GOROOT=")

	output, err := command.Output()
	if err != nil {
		t.Fatal("cannot identify the active Go toolchain root")
	}

	path := strings.TrimSpace(string(output))
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n") {
		t.Fatal("invalid Go toolchain root")
	}

	return path
}

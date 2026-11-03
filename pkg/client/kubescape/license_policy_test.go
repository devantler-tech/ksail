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
	"time"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
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
	goRoot, goVersion := activeLicenseToolchain(t, root)
	t.Setenv("GOROOT", goRoot)

	for key, value := range map[string]string{
		"GOFLAGS": "-mod=readonly", "GOWORK": "off",
		"GOENV": "off", "GOEXPERIMENT": "", "GOPACKAGESDRIVER": "off",
	} {
		t.Setenv(key, value)
	}

	audit := newLicenseNoticeAudit(t, root, goRoot, goVersion)
	generated := map[string]gonotices.Payload{}

	for _, target := range noticeAuditTargets(t, root) {
		t.Run(target.name, func(t *testing.T) {
			t.Setenv("GOOS", target.platform.goos)
			t.Setenv("GOARCH", target.platform.goarch)
			t.Setenv("CGO_ENABLED", target.cgo)
			generated[target.name] = audit.targetPayload(t, target)
		})
	}

	if t.Failed() {
		return
	}

	audit.verifyReceipt(t, generated)
}

type licenseNoticeAudit struct {
	root           string
	classifier     *strictLicenseClassifier
	notices        *gonotices.Collector
	policy         licensepolicy.Policy
	materialPolicy gonotices.MaterialPolicy
	toolchain      gonotices.Toolchain
	update         bool
}

func newLicenseNoticeAudit(t *testing.T, root, goRoot, goVersion string) *licenseNoticeAudit {
	t.Helper()

	base, err := licenses.NewClassifier()
	if err != nil {
		t.Fatal("cannot construct pinned license classifier")
	}

	audit := &licenseNoticeAudit{
		root:       root,
		classifier: &strictLicenseClassifier{delegate: base},
		notices:    new(gonotices.Collector),
		policy: reviewedGoLicensePolicy(
			t,
			root,
		),
		update: os.Getenv("KSAIL_UPDATE_GO_NOTICES") == "1",
	}

	if audit.update && os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Fatal("hosted CI must compare committed recipient material, not regenerate it")
	}

	audit.materialPolicy, err = gonotices.LoadMaterialPolicy(root)
	if err != nil {
		t.Fatal("recipient material decisions are malformed or missing")
	}

	audit.toolchain = audit.selectedToolchain(t, goRoot, goVersion)

	return audit
}

func (audit *licenseNoticeAudit) selectedToolchain(
	t *testing.T,
	goRoot, goVersion string,
) gonotices.Toolchain {
	t.Helper()

	license := selectedToolchainLicense(t, goRoot, goVersion)

	terms, err := audit.classifier.Identify(license)
	if err != nil || len(terms) == 0 {
		t.Fatal("actual Go toolchain license classification failed")
	}

	names := make([]string, 0, len(terms))
	for _, term := range terms {
		if !slices.Contains(audit.policy.Allowed, term.Name) || term.Name == "Unknown" {
			t.Fatal("actual Go toolchain license is unapproved")
		}

		names = append(names, term.Name)
	}

	return gonotices.Toolchain{
		Directory:   goRoot,
		Version:     goVersion,
		LicenseFile: license,
		Licenses:    names,
	}
}

func noticeAuditTargets(t *testing.T, root string) []licenseTarget {
	t.Helper()

	targets := licenseTargets(t, root)

	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.name)
	}

	err := gonotices.ValidateTargets(names)
	if err != nil {
		t.Fatal("recipient Go target coverage changed")
	}

	return targets
}

func (audit *licenseNoticeAudit) targetPayload(
	t *testing.T,
	target licenseTarget,
) gonotices.Payload {
	t.Helper()

	started := time.Now()
	packages := audit.targetPackages(t, target)

	libraries, err := licenses.Libraries(t.Context(), audit.classifier, false, nil, target.entry)
	if err != nil || audit.classifier.hadFailure() {
		t.Fatal("license inventory or source-file classification failed")
	}

	if len(libraries) == 0 {
		t.Fatal("license inventory examined no libraries")
	}

	rows, noticePackages := evaluateLicenseInventory(
		t, audit.root, audit.policy, packages, libraries, audit.classifier, audit.materialPolicy,
	)
	writeLicenseReport(t, target.name, rows)

	loaded := time.Now()
	t.Logf("source inventory phase: %s", loaded.Sub(started))

	if t.Failed() {
		t.Fatal("unapproved source cannot produce recipient material")
	}

	toolchain := noticeTargetToolchain(audit.toolchain, packages)

	payload, err := audit.notices.Build(gonotices.Input{
		Root: audit.root, Target: target.name, Packages: noticePackages, Toolchain: &toolchain,
	})
	if err != nil {
		t.Fatal(err)
	}

	audit.storePayload(t, target.name, payload)
	t.Logf("source inventory %s; recipient material %s; text=%d manifest=%d bytes",
		loaded.Sub(started), time.Since(loaded), len(payload.Text), len(payload.Manifest))

	return payload
}

func (audit *licenseNoticeAudit) targetPackages(
	t *testing.T,
	target licenseTarget,
) []goListPackage {
	t.Helper()

	flags := "-mod=readonly"
	if target.tags != "" {
		flags += " -tags=" + target.tags
	}

	t.Setenv("GOFLAGS", flags)

	packages := listDependencyEntry(
		t,
		audit.root,
		target.platform,
		target.cgo,
		target.tags,
		target.entry,
	)
	for _, pkg := range packages {
		if len(pkg.SysoFiles) != 0 {
			t.Fatalf(
				"opaque compiled input requires a recipient-material disposition: %s %v",
				pkg.ImportPath,
				pkg.SysoFiles,
			)
		}

		if len(pkg.SFiles)+len(compiledNoticeFiles(pkg)) != 0 {
			t.Logf("selected non-Go source: package=%s standard=%t assembly=%v other=%v",
				pkg.ImportPath, pkg.Standard, pkg.SFiles, compiledNoticeFiles(pkg))
		}
	}

	return packages
}

func noticeTargetToolchain(base gonotices.Toolchain, packages []goListPackage) gonotices.Toolchain {
	selected := base
	selected.Licenses = slices.Clone(base.Licenses)
	selected.Packages = nil

	for _, pkg := range packages {
		if pkg.Standard {
			selected.Packages = append(selected.Packages, gonotices.ToolchainPackage{
				ImportPath: pkg.ImportPath,
				Directory:  pkg.Dir,
				GoFiles:    append(slices.Clone(pkg.GoFiles), pkg.CgoFiles...),
				AssemblyFiles: slices.Clone(
					pkg.SFiles,
				),
				OtherFiles: compiledNoticeFiles(pkg),
				EmbedFiles: slices.Clone(pkg.EmbedFiles),
			})
		}
	}

	return selected
}

func (audit *licenseNoticeAudit) storePayload(
	t *testing.T,
	target string,
	payload gonotices.Payload,
) {
	t.Helper()

	var err error
	if audit.update {
		err = gonotices.Write(audit.root, target, payload)
	} else {
		err = gonotices.Verify(audit.root, target, payload)
	}

	if err != nil {
		t.Fatal(
			"recipient Go material is missing, stale or unsafe; regenerate through the audited owner",
		)
	}
}

func (audit *licenseNoticeAudit) verifyReceipt(
	t *testing.T,
	generated map[string]gonotices.Payload,
) {
	t.Helper()

	var err error
	if audit.update {
		err = gonotices.WriteReceipt(audit.root, generated)
	} else {
		err = gonotices.VerifyReceipt(audit.root, generated)
	}

	if err != nil {
		t.Fatal(
			"eight-target recipient hash receipt is missing or stale; regenerate through the audited owner",
		)
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
	classifier *strictLicenseClassifier,
	materialPolicy gonotices.MaterialPolicy,
) ([]licenseReportRow, []gonotices.Package) {
	t.Helper()

	expected, err := completeLicenseMembership(packages, libraries)
	if err != nil {
		t.Fatal(err)
	}

	var rows []licenseReportRow

	var noticePackages []gonotices.Package

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

			noticePackage := inventoryNoticePackage(
				t,
				root,
				policy,
				pkg,
				library.LicenseFile,
				raw,
				effective,
				classifier,
				materialPolicy,
			)
			noticePackages = append(noticePackages, noticePackage)

			if err != nil {
				t.Errorf("unapproved dependency license: %s (%v)", name, raw)
			}
		}
	}

	slices.SortFunc(rows, func(a, b licenseReportRow) int {
		return strings.Compare(a.Package, b.Package)
	})

	return rows, noticePackages
}

func inventoryNoticePackage(
	t *testing.T,
	root string,
	policy licensepolicy.Policy,
	pkg goListPackage,
	licenseFile string,
	raw, effective []string,
	classifier *strictLicenseClassifier,
	materialPolicy gonotices.MaterialPolicy,
) gonotices.Package {
	t.Helper()

	noticePackage := selectedNoticePackage(policy, pkg, licenseFile, raw, effective)
	if classifier == nil || pkg.Module.Main {
		return noticePackage
	}

	result, err := evaluateNoticeDeclarations(
		root,
		policy,
		pkg,
		noticePackage,
		classifier,
		materialPolicy,
	)
	if err != nil {
		t.Errorf("unresolved recipient declaration: %s: %v", pkg.ImportPath, err)
	}

	return result
}

func selectedNoticePackage(
	policy licensepolicy.Policy,
	pkg goListPackage,
	licenseFile string,
	raw, effective []string,
) gonotices.Package {
	result := gonotices.Package{
		ImportPath:  pkg.ImportPath,
		Directory:   pkg.Dir,
		LicenseFile: licenseFile,
		GoFiles: append(
			slices.Clone(pkg.GoFiles),
			pkg.CgoFiles...),
		EmbedFiles:    slices.Clone(pkg.EmbedFiles),
		AssemblyFiles: slices.Clone(pkg.SFiles),
		OtherFiles:    compiledNoticeFiles(pkg),
		Raw: slices.Clone(
			raw,
		),
		Effective: slices.Clone(effective),
		Module:    noticeModule(pkg.Module),
	}

	for _, exception := range policy.Exceptions {
		if slices.Contains(exception.Packages, pkg.ImportPath) {
			result.EvidenceFiles = append(result.EvidenceFiles, exception.Evidence)
			result.UpstreamTermsURL = exception.SourceURL
		}
	}

	return result
}

func compiledNoticeFiles(pkg goListPackage) []string {
	var files []string

	for _, group := range [][]string{pkg.CFiles, pkg.HFiles, pkg.CXXFiles, pkg.MFiles, pkg.FFiles} {
		files = append(files, group...)
	}

	slices.Sort(files)

	return slices.Compact(files)
}

func noticeModule(selected *goListModule) gonotices.Module {
	result := gonotices.Module{
		Path:      selected.Path,
		Version:   selected.Version,
		Sum:       selected.Sum,
		Directory: selected.Dir,
		Main:      selected.Main,
	}
	if selected.Replace != nil {
		replaced := noticeModule(selected.Replace)
		result.Replace = &replaced
	}

	return result
}

func evaluateNoticeDeclarations(
	root string,
	policy licensepolicy.Policy,
	source goListPackage,
	pkg gonotices.Package,
	classifier *strictLicenseClassifier,
	materialPolicy gonotices.MaterialPolicy,
) (gonotices.Package, error) {
	paths, err := gonotices.SupplementalPaths(pkg)
	if err != nil {
		return pkg, errLicenseInventory
	}

	directory := source.Module.Dir
	if source.Module.Replace != nil {
		directory = source.Module.Replace.Dir
	}

	for _, path := range paths {
		raw, identifyErr := supplementalLicenseNames(classifier, directory, path)
		if identifyErr != nil {
			return pkg, identifyErr
		}

		if materialPolicy.Covers(pkg, path) {
			declaration, resolveErr := materialPolicy.Resolve(pkg, path, raw, policy.Allowed)
			if resolveErr != nil {
				return pkg, fmt.Errorf(
					"%w: recipient decision for supplemental %s (%v)",
					errLicenseInventory,
					path,
					raw,
				)
			}

			pkg.Declarations = append(pkg.Declarations, declaration)

			continue
		}

		effective, evaluateErr := licensepolicy.Evaluate(
			policy,
			root,
			selectedLicenseFinding(
				source,
				source.ImportPath,
				filepath.Join(directory, filepath.FromSlash(path)),
				raw,
			),
		)
		if evaluateErr != nil {
			return pkg, fmt.Errorf("%w: supplemental %s (%v)", evaluateErr, path, raw)
		}

		pkg.Declarations = append(
			pkg.Declarations,
			gonotices.Declaration{Path: path, Raw: raw, Effective: effective},
		)
	}

	return pkg, nil
}

func supplementalLicenseNames(
	classifier *strictLicenseClassifier,
	directory, path string,
) ([]string, error) {
	identified, err := classifier.Identify(filepath.Join(directory, filepath.FromSlash(path)))
	if err != nil {
		return nil, fmt.Errorf("%w: supplemental %s", errLicenseInventory, path)
	}

	raw := make([]string, 0, len(identified))
	for _, license := range identified {
		raw = append(raw, license.Name)
	}

	if len(raw) == 0 {
		raw = []string{"Unknown"}
	}

	return raw, nil
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

func activeLicenseToolchain(t *testing.T, root string) (string, string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "go", "env", "GOROOT", "GOVERSION")
	command.Dir = root
	command.Env = auditGoEnv("GOROOT=")

	output, err := command.Output()
	if err != nil {
		t.Fatal("cannot identify the active Go toolchain root")
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 2 || !filepath.IsAbs(lines[0]) || strings.ContainsAny(lines[0], "\r") ||
		!strings.HasPrefix(lines[1], "go") {
		t.Fatal("invalid Go toolchain root")
	}

	return lines[0], lines[1]
}

func selectedToolchainLicense(t *testing.T, goRoot, version string) string {
	t.Helper()

	path := filepath.Join(goRoot, "LICENSE")

	info, err := os.Lstat(path)
	if err == nil && info.Mode().IsRegular() {
		return path
	}

	parent := filepath.Dir(goRoot)
	if filepath.Base(goRoot) == "libexec" &&
		filepath.Base(parent) == strings.TrimPrefix(version, "go") {
		path = filepath.Join(parent, "LICENSE")

		info, err = os.Lstat(path)
		if err == nil && info.Mode().IsRegular() {
			return path
		}
	}

	t.Fatal("actual selected Go toolchain license material is missing")

	return ""
}

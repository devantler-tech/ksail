package depcontract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
	gatewayapiconsts "sigs.k8s.io/gateway-api/pkg/consts"
)

const (
	gatewayAPIModulePath = "sigs.k8s.io/gateway-api"
	ciliumModulePath     = "github.com/cilium/cilium"

	// The contracts below read these two files, and neither is a Go source: the shared Go
	// validation skips its tests when only one of them changes. ci.yaml's dependency-contracts
	// filter runs this package for them instead, and internal/ciharness holds that filter to
	// every repository file named in this package.
	dependabotConfigPath = ".github/dependabot.yaml"
	ciliumChartPinPath   = "pkg/svc/installer/cni/cilium/Dockerfile"

	semverMinorUpdate = "version-update:semver-minor"
	semverMajorUpdate = "version-update:semver-major"
	semverPatchUpdate = "version-update:semver-patch"

	// Fixture versions: the release Cilium v1.20.2 is built against, and its neighbours.
	gatewayAPIBuiltAgainst = "v1.6.1"
	gatewayAPINextPatch    = "v1.6.2"
)

var (
	errUnreadableContractInput = errors.New("cannot read a contract input")
	errInvalidVersion          = errors.New("not a semantic version")
	errGatewayAPIUntracked     = errors.New(
		"gateway API releases would not be proposed as updates",
	)
	errGatewayAPIMinorNotHeld = errors.New(
		"a Gateway API minor would be proposed without the Cilium release that supports it",
	)
	errGatewayAPIBundleSkew = errors.New(
		"the linked Gateway API module does not publish the version it is required at",
	)
	errGatewayAPIOffCiliumLine = errors.New(
		"gateway API is not on the line the pinned Cilium release is built against",
	)
	errCiliumChartPinUnreadable = errors.New("cannot read the pinned Cilium release")

	// ciliumChartPin mirrors chartVersion() in pkg/svc/installer/cni/cilium/version.go.
	ciliumChartPin = regexp.MustCompile(`(?m)^FROM\s+quay\.io/cilium/cilium:(v[^\s@]+)`)
)

//nolint:tagliatelle // Dependabot defines these keys in kebab-case.
type dependabotUpdate struct {
	Ecosystem   string             `yaml:"package-ecosystem"`
	Directory   string             `yaml:"directory"`
	Directories []string           `yaml:"directories"`
	Allow       []yaml.Node        `yaml:"allow"`
	Ignore      []dependabotIgnore `yaml:"ignore"`
}

//nolint:tagliatelle // Dependabot defines these keys in kebab-case.
type dependabotIgnore struct {
	Name        string   `yaml:"dependency-name"`
	Versions    []string `yaml:"versions"`
	UpdateTypes []string `yaml:"update-types"`
}

// TestGatewayAPIReleasesReachTheInstaller guards how the Gateway API CRD version is tracked
// (ksail#7472). KSail installs the release of the sigs.k8s.io/gateway-api module it links, so
// that module has to stay a requirement Dependabot proposes updates for.
func TestGatewayAPIReleasesReachTheInstaller(t *testing.T) {
	t.Parallel()

	err := checkGatewayAPITracked(
		readRepositoryFile(t, "go.mod"),
		readRepositoryFile(t, dependabotConfigPath),
		gatewayapiconsts.BundleVersion,
	)
	if err != nil {
		t.Fatal(err)
	}
}

// TestGatewayAPIStaysOnThePinnedCiliumLine guards the installed Gateway API CRDs against the
// Cilium release KSail installs them for (ksail#7472). Cilium names one Gateway API release per
// version in its own go.mod, so that file at the pinned chart version is the reference.
func TestGatewayAPIStaysOnThePinnedCiliumLine(t *testing.T) {
	t.Parallel()

	ciliumVersion, err := ciliumChartVersion(readRepositoryFile(t, ciliumChartPinPath))
	if err != nil {
		t.Fatal(err)
	}

	file, err := modfile.Parse("go.mod", readRepositoryFile(t, "go.mod"), nil)
	if err != nil {
		t.Fatalf("parse go.mod: %v", err)
	}

	installed, err := effectiveVersion(file, gatewayAPIModulePath)
	if err != nil {
		t.Fatal(err)
	}

	err = checkGatewayAPIFollowsCilium(
		installed,
		ciliumVersion,
		releasedGoMod(t, ciliumModulePath, ciliumVersion),
	)
	if err != nil {
		t.Fatal(err)
	}
}

// gatewayAPITrackingCase is one input to checkGatewayAPITracked. A nil goMod stands for a
// direct requirement at the published version, and a nil dependabot for a root gomod entry
// that holds Gateway API minors back and nothing else.
type gatewayAPITrackingCase struct {
	name       string
	goMod      []byte
	dependabot []byte
	wantErr    error
}

func TestCheckGatewayAPITrackedRequirement(t *testing.T) {
	t.Parallel()

	runGatewayAPITrackingCases(t, []gatewayAPITrackingCase{
		{name: "direct requirement at the published version"},
		{
			// The state before ksail#7472: nothing imported the module, so nothing proposed it.
			name:    "indirect requirement",
			goMod:   goModRequiringGatewayAPI(gatewayAPIBuiltAgainst, "// indirect"),
			wantErr: errGatewayAPIUntracked,
		},
		{
			name:    "module not required",
			goMod:   []byte("module example.com/m\n"),
			wantErr: errModuleNotRequired,
		},
		{
			name:    "module publishes another version than its tag",
			goMod:   goModRequiringGatewayAPI(gatewayAPINextPatch, ""),
			wantErr: errGatewayAPIBundleSkew,
		},
		{
			name: "replaced by a local directory",
			goMod: goModRequiringGatewayAPI(
				gatewayAPIBuiltAgainst,
				"",
				gatewayAPIModulePath+" => ./third_party/gateway-api",
			),
			wantErr: errIncomparableReplace,
		},
		{
			name:    "unparseable go.mod",
			goMod:   []byte("module\n"),
			wantErr: errUnreadableContractInput,
		},
	})
}

func TestCheckGatewayAPITrackedDependabotEntry(t *testing.T) {
	t.Parallel()

	const heldMinor = `    ignore:
      - dependency-name: "sigs.k8s.io/gateway-api"
        update-types: ["version-update:semver-minor"]
`

	runGatewayAPITrackingCases(t, []gatewayAPITrackingCase{
		{
			name: "root entry declared through directories",
			dependabot: []byte(
				"updates:\n  - package-ecosystem: gomod\n    directories: [\"/\"]\n" + heldMinor,
			),
		},
		{
			name: "no gomod entry for the repository root",
			dependabot: []byte(
				"updates:\n  - package-ecosystem: docker\n    directory: /\n" + heldMinor +
					"  - package-ecosystem: gomod\n    directory: /tools\n" + heldMinor,
			),
			wantErr: errGatewayAPIUntracked,
		},
		{
			name: "allow list is not evaluated",
			dependabot: []byte(
				"updates:\n  - package-ecosystem: gomod\n    directory: /\n" +
					"    allow:\n      - dependency-name: \"k8s.io/*\"\n" + heldMinor,
			),
			wantErr: errGatewayAPIUntracked,
		},
		{
			name:       "no updates",
			dependabot: []byte("version: 2\n"),
			wantErr:    errGatewayAPIUntracked,
		},
		{
			name:       "unparseable configuration",
			dependabot: []byte("updates: [\n"),
			wantErr:    errUnreadableContractInput,
		},
	})
}

func TestCheckGatewayAPITrackedIgnoreRulesThatKeepTracking(t *testing.T) {
	t.Parallel()

	runGatewayAPITrackingCases(t, []gatewayAPITrackingCase{
		{
			name: "minor and major held back",
			dependabot: dependabotWithIgnore(
				gatewayAPIModulePath, semverMinorUpdate, semverMajorUpdate,
			),
		},
		{
			name:       "minor held back by a wildcard rule",
			dependabot: dependabotWithIgnore("sigs.k8s.io/*", semverMinorUpdate),
		},
		{
			name:       "minor not held back",
			dependabot: dependabotWithIgnore("k8s.io/*", semverMinorUpdate),
			wantErr:    errGatewayAPIMinorNotHeld,
		},
		{
			name:       "only major held back",
			dependabot: dependabotWithIgnore(gatewayAPIModulePath, semverMajorUpdate),
			wantErr:    errGatewayAPIMinorNotHeld,
		},
	})
}

func TestCheckGatewayAPITrackedIgnoreRulesThatStopTracking(t *testing.T) {
	t.Parallel()

	runGatewayAPITrackingCases(t, []gatewayAPITrackingCase{
		{
			name:       "every update ignored",
			dependabot: dependabotWithIgnore(gatewayAPIModulePath),
			wantErr:    errGatewayAPIUntracked,
		},
		{
			name:       "every update ignored by a wildcard rule",
			dependabot: dependabotWithIgnore("sigs.k8s.io/*"),
			wantErr:    errGatewayAPIUntracked,
		},
		{
			name:       "every update ignored by a rule naming no dependency",
			dependabot: dependabotWithIgnore(""),
			wantErr:    errGatewayAPIUntracked,
		},
		{
			name: "patch updates ignored",
			dependabot: dependabotWithIgnore(
				gatewayAPIModulePath, semverMinorUpdate, semverPatchUpdate,
			),
			wantErr: errGatewayAPIUntracked,
		},
		{
			name: "version ranges are not evaluated",
			dependabot: []byte(
				"updates:\n  - package-ecosystem: gomod\n    directory: /\n    ignore:\n" +
					"      - dependency-name: \"sigs.k8s.io/gateway-api\"\n" +
					"        versions: [\">= 1.6.2\"]\n",
			),
			wantErr: errGatewayAPIUntracked,
		},
	})
}

func runGatewayAPITrackingCases(t *testing.T, testCases []gatewayAPITrackingCase) {
	t.Helper()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			goMod := testCase.goMod
			if goMod == nil {
				goMod = goModRequiringGatewayAPI(gatewayAPIBuiltAgainst, "")
			}

			dependabot := testCase.dependabot
			if dependabot == nil {
				dependabot = dependabotWithIgnore(gatewayAPIModulePath, semverMinorUpdate)
			}

			err := checkGatewayAPITracked(goMod, dependabot, gatewayAPIBuiltAgainst)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("checkGatewayAPITracked() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

// gatewayAPICiliumCase is one input to checkGatewayAPIFollowsCilium. A nil ciliumGoMod stands
// for a Cilium release built against gatewayAPIBuiltAgainst.
type gatewayAPICiliumCase struct {
	name        string
	installed   string
	ciliumGoMod []byte
	wantErr     error
}

func TestCheckGatewayAPIFollowsCiliumLine(t *testing.T) {
	t.Parallel()

	runGatewayAPICiliumCases(t, []gatewayAPICiliumCase{
		{name: "same release", installed: gatewayAPIBuiltAgainst},
		{name: "newer patch on the same line", installed: gatewayAPINextPatch},
		{
			// Cilium v1.19.5 requires a release candidate; the final release is on its line.
			name:        "final release of the candidate Cilium is built against",
			installed:   "v1.4.1",
			ciliumGoMod: goModRequiringGatewayAPI("v1.4.0-rc.2", ""),
		},
		{
			// What KSail installed for Cilium v1.20.2 before ksail#7472.
			name:      "older line",
			installed: "v1.5.1",
			wantErr:   errGatewayAPIOffCiliumLine,
		},
		{
			name:      "older patch on the same line",
			installed: "v1.6.0",
			wantErr:   errGatewayAPIOffCiliumLine,
		},
		{
			name:      "candidate older than the release Cilium is built against",
			installed: "v1.6.1-rc.1",
			wantErr:   errGatewayAPIOffCiliumLine,
		},
		{name: "newer line", installed: "v1.7.0", wantErr: errGatewayAPIOffCiliumLine},
	})
}

func TestCheckGatewayAPIFollowsCiliumUnreadableInputs(t *testing.T) {
	t.Parallel()

	runGatewayAPICiliumCases(t, []gatewayAPICiliumCase{
		{name: "installed version unreadable", installed: "1.6.1", wantErr: errInvalidVersion},
		{name: "installed version missing", installed: "", wantErr: errInvalidVersion},
		{
			name:        "Cilium does not require Gateway API",
			installed:   gatewayAPIBuiltAgainst,
			ciliumGoMod: []byte("module " + ciliumModulePath + "\n"),
			wantErr:     errModuleNotRequired,
		},
		{
			name:      "Cilium builds against a fork",
			installed: gatewayAPIBuiltAgainst,
			ciliumGoMod: goModRequiringGatewayAPI(
				gatewayAPIBuiltAgainst,
				"",
				gatewayAPIModulePath+" => example.com/fork/gateway-api "+gatewayAPIBuiltAgainst,
			),
			wantErr: errIncomparableReplace,
		},
		{
			name:        "unparseable Cilium go.mod",
			installed:   gatewayAPIBuiltAgainst,
			ciliumGoMod: []byte("module\n"),
			wantErr:     errUnreadableContractInput,
		},
	})
}

func runGatewayAPICiliumCases(t *testing.T, testCases []gatewayAPICiliumCase) {
	t.Helper()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ciliumGoMod := testCase.ciliumGoMod
			if ciliumGoMod == nil {
				ciliumGoMod = goModRequiringGatewayAPI(gatewayAPIBuiltAgainst, "")
			}

			err := checkGatewayAPIFollowsCilium(testCase.installed, "v1.20.2", ciliumGoMod)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf(
					"checkGatewayAPIFollowsCilium() error = %v, want %v",
					err,
					testCase.wantErr,
				)
			}
		})
	}
}

func TestCiliumChartVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		dockerfile string
		want       string
		wantErr    error
	}{
		{
			name:       "tag pinned by digest",
			dockerfile: "# comment\n\nFROM quay.io/cilium/cilium:v1.20.2@sha256:2939231d\n",
			want:       "v1.20.2",
		},
		{
			name:       "tag without a digest",
			dockerfile: "FROM quay.io/cilium/cilium:v1.21.0\n",
			want:       "v1.21.0",
		},
		{
			name:       "no Cilium image",
			dockerfile: "FROM quay.io/cilium/operator:v1.20.2\n",
			wantErr:    errCiliumChartPinUnreadable,
		},
		{name: "empty file", dockerfile: "", wantErr: errCiliumChartPinUnreadable},
		{
			name:       "tag that is not a release",
			dockerfile: "FROM quay.io/cilium/cilium:vnext\n",
			wantErr:    errInvalidVersion,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := ciliumChartVersion([]byte(testCase.dockerfile))
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("ciliumChartVersion() error = %v, want %v", err, testCase.wantErr)
			}

			if got != testCase.want {
				t.Fatalf("ciliumChartVersion() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// goModRequiringGatewayAPI renders a go.mod requiring sigs.k8s.io/gateway-api at version,
// followed by comment on the same line and one replace directive per entry.
func goModRequiringGatewayAPI(version, comment string, replacements ...string) []byte {
	var builder strings.Builder

	fmt.Fprintf(
		&builder,
		"module example.com/m\n\nrequire %s %s %s\n",
		gatewayAPIModulePath, version, comment,
	)

	for _, replacement := range replacements {
		fmt.Fprintf(&builder, "\nreplace %s\n", replacement)
	}

	return []byte(builder.String())
}

// dependabotWithIgnore renders a Dependabot configuration whose root gomod entry has one ignore
// rule: for dependency, limited to updateTypes. No update type renders a rule that ignores
// every update, and an empty dependency renders one that names no dependency at all.
func dependabotWithIgnore(dependency string, updateTypes ...string) []byte {
	var builder strings.Builder

	builder.WriteString("updates:\n  - package-ecosystem: gomod\n    directory: /\n    ignore:\n")

	if dependency == "" {
		builder.WriteString("      - versions: [\">= 0\"]\n")
	} else {
		builder.WriteString("      - dependency-name: " + strconv.Quote(dependency) + "\n")
	}

	if len(updateTypes) > 0 {
		quoted := make([]string, 0, len(updateTypes))
		for _, updateType := range updateTypes {
			quoted = append(quoted, strconv.Quote(updateType))
		}

		builder.WriteString("        update-types: [" + strings.Join(quoted, ", ") + "]\n")
	}

	return []byte(builder.String())
}

// checkGatewayAPITracked reports whether a new sigs.k8s.io/gateway-api release reaches this
// repository as a proposed update, and whether the proposed version is the one KSail installs.
//
// KSail installs the Gateway API CRDs of the module release it links, read from the bundle
// version that module publishes. Dependabot proposes only direct requirements, so the module
// must stay one; and a release whose published bundle version differs from its tag would make
// KSail install something other than what go.mod names. The version used to live in a
// Dockerfile naming an image that stopped at v1.0.0, which nothing could ever update
// (ksail#7472).
func checkGatewayAPITracked(goMod, dependabot []byte, bundleVersion string) error {
	file, err := modfile.Parse("go.mod", goMod, nil)
	if err != nil {
		return fmt.Errorf("%w: parse go.mod: %w", errUnreadableContractInput, err)
	}

	version, err := effectiveVersion(file, gatewayAPIModulePath)
	if err != nil {
		return err
	}

	for _, requirement := range file.Require {
		if requirement.Mod.Path == gatewayAPIModulePath && requirement.Indirect {
			return fmt.Errorf(
				"%w: %s is an indirect requirement, and Dependabot proposes only direct ones. "+
					"Keep the import that makes it direct: pkg/svc/installer/cni/cilium reads "+
					"the version of the CRDs it installs from this module (ksail#7472)",
				errGatewayAPIUntracked, gatewayAPIModulePath,
			)
		}
	}

	if version != bundleVersion {
		return fmt.Errorf(
			"%w: go.mod requires %s %s, but that release publishes bundle version %s, which is "+
				"what KSail would install (ksail#7472)",
			errGatewayAPIBundleSkew, gatewayAPIModulePath, version, bundleVersion,
		)
	}

	return checkDependabotProposesGatewayAPI(dependabot)
}

// checkDependabotProposesGatewayAPI reports whether the Dependabot configuration in contents
// still proposes Gateway API patch releases for the root module while holding minors back.
//
// Cilium is built against one Gateway API line per release, so a minor arrives with the Cilium
// release that supports it and is otherwise held back here. A rule that ignores more than that
// would stop the releases KSail does want.
func checkDependabotProposesGatewayAPI(contents []byte) error {
	var config struct {
		Updates []dependabotUpdate `yaml:"updates"`
	}

	err := yaml.Unmarshal(contents, &config)
	if err != nil {
		return fmt.Errorf("%w: parse %s: %w", errUnreadableContractInput, dependabotConfigPath, err)
	}

	root := rootGoModulesUpdate(config.Updates)
	if root == nil {
		return fmt.Errorf(
			"%w: %s has no gomod entry for the repository root",
			errGatewayAPIUntracked, dependabotConfigPath,
		)
	}

	if len(root.Allow) > 0 {
		return fmt.Errorf(
			"%w: the root gomod entry in %s has an allow list, which this check does not "+
				"evaluate. Teach it to before adding one",
			errGatewayAPIUntracked, dependabotConfigPath,
		)
	}

	minorHeld := false

	for _, rule := range root.Ignore {
		holdsMinor, ruleErr := gatewayAPIIgnoreRule(rule)
		if ruleErr != nil {
			return ruleErr
		}

		minorHeld = minorHeld || holdsMinor
	}

	if !minorHeld {
		return fmt.Errorf(
			"%w: add an ignore rule for %s with update type %s to the root gomod entry in %s",
			errGatewayAPIMinorNotHeld, gatewayAPIModulePath, semverMinorUpdate,
			dependabotConfigPath,
		)
	}

	return nil
}

// rootGoModulesUpdate returns the Dependabot entry that updates the root go.mod, or nil.
func rootGoModulesUpdate(updates []dependabotUpdate) *dependabotUpdate {
	for index := range updates {
		update := &updates[index]
		if update.Ecosystem != "gomod" {
			continue
		}

		if update.Directory == "/" || slices.Contains(update.Directories, "/") {
			return update
		}
	}

	return nil
}

// gatewayAPIIgnoreRule reports whether rule holds Gateway API minors back. It returns an error
// when the rule applies to Gateway API and ignores anything other than minor and major updates,
// or ignores version ranges, which this check does not evaluate.
func gatewayAPIIgnoreRule(rule dependabotIgnore) (bool, error) {
	if !dependencyPatternMatches(rule.Name, gatewayAPIModulePath) {
		return false, nil
	}

	if len(rule.Versions) > 0 || len(rule.UpdateTypes) == 0 {
		return false, fmt.Errorf(
			"%w: the ignore rule for %q in %s names versions or no update types. Limit it to "+
				"update types %s and %s",
			errGatewayAPIUntracked, rule.Name, dependabotConfigPath,
			semverMinorUpdate, semverMajorUpdate,
		)
	}

	holdsMinor := false

	for _, updateType := range rule.UpdateTypes {
		switch updateType {
		case semverMinorUpdate:
			holdsMinor = true
		case semverMajorUpdate:
			// A major is a different module path under Go's rules, so holding it changes nothing.
		default:
			return false, fmt.Errorf(
				"%w: the ignore rule for %q in %s also ignores %s",
				errGatewayAPIUntracked, rule.Name, dependabotConfigPath, updateType,
			)
		}
	}

	return holdsMinor, nil
}

// dependencyPatternMatches reports whether a Dependabot dependency-name pattern, in which *
// stands for any run of characters, applies to name. A rule that names no dependency applies
// to every dependency.
func dependencyPatternMatches(pattern, name string) bool {
	if pattern == "" {
		return true
	}

	parts := strings.Split(pattern, "*")
	for index, part := range parts {
		parts[index] = regexp.QuoteMeta(part)
	}

	matched, err := regexp.MatchString("^"+strings.Join(parts, ".*")+"$", name)

	return err == nil && matched
}

// checkGatewayAPIFollowsCilium reports whether installed, the Gateway API release KSail
// installs, is on the line that Cilium at ciliumVersion is built against: the same minor, and
// no older than the release ciliumGoMod, Cilium's own go.mod at that version, requires.
//
// Cilium documents that requirement as the Gateway API version each release supports, and its
// operator serves Gateway API only when the CRD versions it expects are installed. A minor
// ahead is not a release Cilium was tested with, and a minor behind is what KSail installed
// unnoticed for Cilium v1.20 (ksail#7472).
func checkGatewayAPIFollowsCilium(installed, ciliumVersion string, ciliumGoMod []byte) error {
	file, err := modfile.Parse("go.mod", ciliumGoMod, nil)
	if err != nil {
		return fmt.Errorf(
			"%w: parse the go.mod of %s %s: %w",
			errUnreadableContractInput, ciliumModulePath, ciliumVersion, err,
		)
	}

	required, err := effectiveVersion(file, gatewayAPIModulePath)
	if err != nil {
		return fmt.Errorf("%s %s: %w", ciliumModulePath, ciliumVersion, err)
	}

	for _, version := range []string{installed, required} {
		if !semver.IsValid(version) {
			return fmt.Errorf("%w: %s %q", errInvalidVersion, gatewayAPIModulePath, version)
		}
	}

	onLine := semver.MajorMinor(installed) == semver.MajorMinor(required)
	if onLine && semver.Compare(installed, required) >= 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: KSail installs the Gateway API %s CRDs, but Cilium %s, pinned in %s, is built "+
			"against %s. Move %s to the %s line at or above %s, e.g. `go get %s@%s`, in the "+
			"change that moves either pin (ksail#7472)",
		errGatewayAPIOffCiliumLine, installed, ciliumVersion, ciliumChartPinPath, required,
		gatewayAPIModulePath, semver.MajorMinor(required), required,
		gatewayAPIModulePath, required,
	)
}

// ciliumChartVersion returns the Cilium release pinned in dockerfile, the file the installer
// reads its chart version from.
func ciliumChartVersion(dockerfile []byte) (string, error) {
	match := ciliumChartPin.FindSubmatch(dockerfile)
	if match == nil {
		return "", fmt.Errorf(
			"%w: %s has no `FROM quay.io/cilium/cilium:<tag>` line. Update this check together "+
				"with the installer's version.go",
			errCiliumChartPinUnreadable, ciliumChartPinPath,
		)
	}

	version := string(match[1])
	if !semver.IsValid(version) {
		return "", fmt.Errorf(
			"%w: %s pins Cilium %q",
			errInvalidVersion, ciliumChartPinPath, version,
		)
	}

	return version, nil
}

// releasedGoMod returns the go.mod that modulePath published at version. The go command reads
// it from the module cache when the build already uses that version, and from the module proxy
// otherwise, which is the case on a change that moves the Cilium chart pin alone. A lookup that
// fails also fails the test: an unread go.mod says nothing about the release.
func releasedGoMod(t *testing.T, modulePath, version string) []byte {
	t.Helper()

	//nolint:gosec // G204: a fixed module path and a version validated as a semantic version.
	command := exec.CommandContext(
		t.Context(), "go", "list", "-m", "-json", modulePath+"@"+version,
	)
	command.Dir = filepath.Join("..", "..")

	command.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")

	var stderr bytes.Buffer

	command.Stderr = &stderr

	output, err := command.Output()
	if err != nil {
		t.Fatalf("resolve %s@%s: %v\n%s", modulePath, version, err, stderr.String())
	}

	var module struct {
		Version string `json:"Version"` //nolint:tagliatelle // Go command output contract.
		GoMod   string `json:"GoMod"`   //nolint:tagliatelle // Go command output contract.
	}

	err = json.Unmarshal(output, &module)
	if err != nil {
		t.Fatalf("decode the go command's answer for %s@%s: %v", modulePath, version, err)
	}

	if module.Version != version || module.GoMod == "" {
		t.Fatalf(
			"the go command did not return the go.mod of %s@%s: version %q, go.mod %q",
			modulePath, version, module.Version, module.GoMod,
		)
	}

	contents, err := os.ReadFile(module.GoMod)
	if err != nil {
		t.Fatalf("read the go.mod of %s@%s: %v", modulePath, version, err)
	}

	return contents
}

// readRepositoryFile returns the contents of path, relative to the repository root.
func readRepositoryFile(t *testing.T, path string) []byte {
	t.Helper()

	// The callers pass fixed repository paths, never input.
	contents, err := os.ReadFile(filepath.Join("..", "..", path)) //nolint:gosec
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return contents
}

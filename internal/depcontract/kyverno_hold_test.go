package depcontract_test

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

const (
	// kyvernoChartPinPath is read by the contracts below; ci.yaml's dependency-contracts
	// filter runs this package when only it changes.
	kyvernoChartPinPath = "pkg/svc/installer/kyverno/Dockerfile"

	kyvernoInstallerDirectory = "/pkg/svc/installer/kyverno"
	kyvernoDependencyName     = "kyverno/charts/kyverno"

	// kyvernoHeldChart is the first chart on the Kyverno 1.20 line, which removes the
	// kyverno.io ClusterPolicy and Policy APIs (ksail#7848).
	kyvernoHeldChart = "3.10.0"

	// kyvernoHoldRange is the exact ignore range the hold uses.
	kyvernoHoldRange = ">= " + kyvernoHeldChart
)

var (
	errKyvernoChartPinUnreadable = errors.New("cannot read the pinned Kyverno chart")
	errKyvernoPastHold           = errors.New(
		"cluster create would install Kyverno 1.20 or later, which removes ClusterPolicy and Policy",
	)
	errKyvernoHoldMissing = errors.New(
		"dependabot would propose the Kyverno 1.20 chart line",
	)

	// kyvernoChartPin mirrors the FROM line pkg/svc/installer/kyverno reads its chart version from.
	kyvernoChartPin = regexp.MustCompile(`(?m)^FROM\s+ghcr\.io/kyverno/charts/kyverno:([^\s@]+)`)
)

// TestKyvernoChartStaysBelowTheHold guards `ksail cluster create` against installing the
// Kyverno 1.20 line before consumers can choose it (ksail#7848). Lifting the hold is a
// decision recorded on that issue, and the release that ships it must say so in its notes.
func TestKyvernoChartStaysBelowTheHold(t *testing.T) {
	t.Parallel()

	err := checkKyvernoChartBelowHold(readRepositoryFile(t, kyvernoChartPinPath))
	if err != nil {
		t.Fatal(err)
	}
}

// TestDependabotHoldsTheKyvernoLine guards the Dependabot rule that keeps the Kyverno 1.20
// chart line from being proposed while 1.19 patches still come through (ksail#7848).
func TestDependabotHoldsTheKyvernoLine(t *testing.T) {
	t.Parallel()

	err := checkKyvernoHoldConfigured(readRepositoryFile(t, dependabotConfigPath))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckKyvernoChartBelowHold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		wantErr    error
	}{
		{name: "1.19 patch line", dockerfile: kyvernoDockerfile("3.9.2"), wantErr: nil},
		{name: "last 1.19 chart", dockerfile: kyvernoDockerfile("3.9.99"), wantErr: nil},
		{
			name:       "first 1.20 chart",
			dockerfile: kyvernoDockerfile("3.10.0"),
			wantErr:    errKyvernoPastHold,
		},
		{
			name:       "later chart major",
			dockerfile: kyvernoDockerfile("4.0.0"),
			wantErr:    errKyvernoPastHold,
		},
		{
			name:       "1.20 release candidate",
			dockerfile: kyvernoDockerfile("3.10.0-rc.1"),
			wantErr:    errKyvernoPastHold,
		},
		{name: "no FROM line", dockerfile: "# empty\n", wantErr: errKyvernoChartPinUnreadable},
		{
			name:       "not a version",
			dockerfile: kyvernoDockerfile("latest"),
			wantErr:    errKyvernoChartPinUnreadable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := checkKyvernoChartBelowHold([]byte(test.dockerfile))
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("checkKyvernoChartBelowHold() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestCheckKyvernoHoldConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dependabot string
		wantErr    error
	}{
		{name: "hold in place", dependabot: kyvernoDependabot(heldRule(">= 3.10.0")), wantErr: nil},
		{name: "no ignore rule", dependabot: kyvernoDependabot(""), wantErr: errKyvernoHoldMissing},
		{
			name:       "hold starts later",
			dependabot: kyvernoDependabot(heldRule(">= 3.11.0")),
			wantErr:    errKyvernoHoldMissing,
		},
		{
			name: "rule names another dependency",
			dependabot: kyvernoDependabot(`
    ignore:
      - dependency-name: "kyverno/charts/kyverno-policies"
        versions: [">= 3.10.0"]`),
			wantErr: errKyvernoHoldMissing,
		},
		{
			// An update-type filter would narrow the hold to some releases only.
			name: "hold narrowed to minors",
			dependabot: kyvernoDependabot(`
    ignore:
      - dependency-name: "kyverno/charts/kyverno"
        versions: [">= 3.10.0"]
        update-types: ["version-update:semver-minor"]`),
			wantErr: errKyvernoHoldMissing,
		},
		{
			name: "no entry for the installer",
			dependabot: `version: 2
updates:
  - package-ecosystem: docker
    directory: /pkg/svc/installer/gatekeeper
`,
			wantErr: errKyvernoHoldMissing,
		},
		{name: "unparseable", dependabot: "updates: [", wantErr: errUnreadableContractInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := checkKyvernoHoldConfigured([]byte(test.dependabot))
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("checkKyvernoHoldConfigured() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

// checkKyvernoChartBelowHold reports whether the chart version pinned in dockerfile is below
// the first chart on the Kyverno 1.20 line. A prerelease of that chart counts as on the line.
func checkKyvernoChartBelowHold(dockerfile []byte) error {
	match := kyvernoChartPin.FindSubmatch(dockerfile)
	if match == nil {
		return fmt.Errorf(
			"%w: no FROM line for ghcr.io/kyverno/charts/kyverno in %s",
			errKyvernoChartPinUnreadable, kyvernoChartPinPath,
		)
	}

	pinned := string(match[1])

	version := "v" + pinned
	if !semver.IsValid(version) {
		return fmt.Errorf("%w: %q is not a semantic version", errKyvernoChartPinUnreadable, pinned)
	}

	// Compare the release line, so a prerelease of the first held chart counts as held.
	if semver.Compare(semver.MajorMinor(version)+".0", "v"+kyvernoHeldChart) >= 0 {
		return fmt.Errorf(
			"%w: %s pins chart %s. Keep it below %s until ksail.yaml can choose the policy "+
				"engine's version or consumers have migrated, record the decision to lift the "+
				"hold on ksail#7848, and name Kyverno 1.20 in the title of the change that lifts "+
				"it so the release notes say so",
			errKyvernoPastHold, kyvernoChartPinPath, pinned, kyvernoHeldChart,
		)
	}

	return nil
}

// checkKyvernoHoldConfigured reports whether the Dependabot entry for the Kyverno installer
// ignores every chart from the first one on the Kyverno 1.20 line, with no narrowing filter.
func checkKyvernoHoldConfigured(contents []byte) error {
	var config struct {
		Updates []dependabotUpdate `yaml:"updates"`
	}

	err := yaml.Unmarshal(contents, &config)
	if err != nil {
		return fmt.Errorf("%w: parse %s: %w", errUnreadableContractInput, dependabotConfigPath, err)
	}

	for _, update := range config.Updates {
		if update.Ecosystem != "docker" ||
			(update.Directory != kyvernoInstallerDirectory &&
				!slices.Contains(update.Directories, kyvernoInstallerDirectory)) {
			continue
		}

		for _, rule := range update.Ignore {
			if rule.Name == kyvernoDependencyName &&
				slices.Equal(rule.Versions, []string{kyvernoHoldRange}) &&
				len(rule.UpdateTypes) == 0 {
				return nil
			}
		}
	}

	return fmt.Errorf(
		"%w: the docker entry for %s in %s needs the ignore rule dependency-name %q with "+
			"versions [%q] and no update-types. Lifting it is a decision recorded on ksail#7848",
		errKyvernoHoldMissing, kyvernoInstallerDirectory, dependabotConfigPath,
		kyvernoDependencyName, kyvernoHoldRange,
	)
}

func kyvernoDockerfile(version string) string {
	return "# Kyverno chart\nFROM ghcr.io/kyverno/charts/kyverno:" + version +
		"@sha256:83054f30148c3cfc001087c1f5772eda7c7224e9b74b7f0e70c39c9cafe009d1\n"
}

func heldRule(versions string) string {
	return `
    ignore:
      - dependency-name: "kyverno/charts/kyverno"
        versions: ["` + versions + `"]`
}

func kyvernoDependabot(ignore string) string {
	return `version: 2
updates:
  - package-ecosystem: docker
    registries: [ghcr]
    directory: /pkg/svc/installer/kyverno` + ignore + "\n"
}

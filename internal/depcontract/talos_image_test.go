package depcontract_test

import (
	"errors"
	"fmt"
	"regexp"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// talosImagePinPath is the file DefaultTalosImage is read from; ci.yaml's
// dependency-contracts filter runs this package when only it changes.
const talosImagePinPath = "pkg/fsutil/configmanager/talos/Dockerfile"

var (
	errTalosImagePinUnreadable = errors.New("cannot read the pinned Talos image")
	errTalosImageSkew          = errors.New(
		"new Talos clusters would boot a different Talos release line than KSail configures",
	)

	// talosImagePin mirrors the FROM line pkg/fsutil/configmanager/talos reads DefaultTalosImage from.
	talosImagePin = regexp.MustCompile(`(?m)^FROM\s+ghcr\.io/siderolabs/talos:([^\s@]+)`)
)

// TestDefaultTalosImageMatchesMachinery guards `ksail cluster create` against booting nodes
// on a Talos release line other than the one whose machinery module writes their
// configuration (ksail#8388).
func TestDefaultTalosImageMatchesMachinery(t *testing.T) {
	t.Parallel()

	err := checkTalosImageMatchesMachinery(
		readRepositoryFile(t, talosImagePinPath),
		readRepositoryFile(t, "go.mod"),
	)
	if err != nil {
		t.Fatal(err)
	}
}

type talosImageCase struct {
	name      string
	image     string
	machinery string
	wantErr   error
}

// talosImageCases returns the cases for the release-line rule. Patch releases on one line share a config
// contract, and Dependabot proposes the image (docker) and the module (gomod) in separate
// pull requests, so patch skew passes.
func talosImageCases() []talosImageCase {
	return []talosImageCase{
		{"same release", "v1.14.6", "v1.14.6", nil},
		{"older patch on the same line", "v1.14.5", "v1.14.6", nil},
		{"newer patch on the same line", "v1.14.7", "v1.14.6", nil},
		{"prerelease image, released module", alpha2, "v1.14.6", errTalosImageSkew},
		{"prerelease image and module", alpha2, alpha2, nil},
		{"image on a newer line", "v1.15.0", "v1.14.6", errTalosImageSkew},
		{"image on an older line", "v1.13.4", "v1.14.6", errTalosImageSkew},
		{"image tag is not a version", "latest", "v1.14.6", errTalosImagePinUnreadable},
	}
}

func TestCheckTalosImageMatchesMachinery(t *testing.T) {
	t.Parallel()

	for _, testCase := range talosImageCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := checkTalosImageMatchesMachinery(
				talosDockerfile(testCase.image),
				goModWith(testCase.machinery, testCase.machinery),
			)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf(
					"checkTalosImageMatchesMachinery() error = %v, want %v",
					err,
					testCase.wantErr,
				)
			}
		})
	}
}

func TestCheckTalosImageMatchesMachineryWithoutFromLine(t *testing.T) {
	t.Parallel()

	err := checkTalosImageMatchesMachinery([]byte("# no image here\n"), goModWith(alpha2, alpha2))
	if !errors.Is(err, errTalosImagePinUnreadable) {
		t.Fatalf(
			"checkTalosImageMatchesMachinery() error = %v, want %v",
			err,
			errTalosImagePinUnreadable,
		)
	}
}

// checkTalosImageMatchesMachinery reports whether the default Talos image pinned in
// dockerfile is on the same release line as the machinery module goMod builds with: the
// same major.minor, and a released image whenever the module is a release. Patch skew is
// allowed, because one line shares a config contract and the two are bumped separately.
func checkTalosImageMatchesMachinery(dockerfile, goMod []byte) error {
	match := talosImagePin.FindSubmatch(dockerfile)
	if match == nil {
		return fmt.Errorf(
			"%w: no FROM line for ghcr.io/siderolabs/talos in %s",
			errTalosImagePinUnreadable, talosImagePinPath,
		)
	}

	image := string(match[1])
	if !semver.IsValid(image) {
		return fmt.Errorf("%w: %q is not a semantic version", errTalosImagePinUnreadable, image)
	}

	file, err := modfile.Parse("go.mod", goMod, nil)
	if err != nil {
		return fmt.Errorf("%w: parse go.mod: %w", errUnreadableContractInput, err)
	}

	machinery, err := effectiveVersion(file, machineryModulePath)
	if err != nil {
		return err
	}

	sameLine := semver.MajorMinor(image) == semver.MajorMinor(machinery)
	prereleaseOnRelease := semver.Prerelease(image) != "" && semver.Prerelease(machinery) == ""

	if !sameLine || prereleaseOnRelease {
		return fmt.Errorf(
			"%w: %s pins %s but %s is %s. Pin the released image of the module's line, "+
				"by digest, or move the module to the image's line first (ksail#8388)",
			errTalosImageSkew, talosImagePinPath, image, machineryModulePath, machinery,
		)
	}

	return nil
}

// talosDockerfile renders the Talos image pin file at version.
func talosDockerfile(version string) []byte {
	return []byte("FROM ghcr.io/siderolabs/talos:" + version + "@sha256:" +
		"0000000000000000000000000000000000000000000000000000000000000000\n")
}

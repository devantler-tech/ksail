package depcontract_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

const (
	talosModulePath     = "github.com/siderolabs/talos"
	machineryModulePath = "github.com/siderolabs/talos/pkg/machinery"

	alpha2 = "v1.14.0-alpha.2"
	beta1  = "v1.14.0-beta.1"
)

var (
	errModuleNotRequired     = errors.New("module is not required")
	errIncomparableReplace   = errors.New("module is replaced, so its version cannot be compared")
	errTalosMachineryVersion = errors.New(
		"talos and its machinery module are on different versions",
	)
)

type lockstepCase struct {
	name    string
	goMod   []byte
	wantErr error
}

// TestTalosAndMachineryMoveInLockstep guards the root module against a skew between
// github.com/siderolabs/talos and github.com/siderolabs/talos/pkg/machinery (ksail#6734).
func TestTalosAndMachineryMoveInLockstep(t *testing.T) {
	t.Parallel()

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}

	repository := os.DirFS(filepath.Join(filepath.Dir(testFile), "..", ".."))

	contents, err := fs.ReadFile(repository, "go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	err = checkTalosLockstep(contents)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckTalosLockstepVersions(t *testing.T) {
	t.Parallel()

	runLockstepCases(t, []lockstepCase{
		{name: "same version", goMod: goModWith(alpha2, alpha2), wantErr: nil},
		{
			name:    "machinery raised alone",
			goMod:   goModWith(alpha2, beta1),
			wantErr: errTalosMachineryVersion,
		},
		{
			name:    "talos raised alone",
			goMod:   goModWith("v1.15.0-alpha.0", alpha2),
			wantErr: errTalosMachineryVersion,
		},
		{
			// The talos group PR #6826 at a4971d2: omni/client pulled machinery past the tag.
			name:    "machinery pseudo-version past the talos tag",
			goMod:   goModWith("v1.15.0-alpha.0", "v1.15.0-alpha.0.0.20260908133727-5c5fd29e95f7"),
			wantErr: errTalosMachineryVersion,
		},
		{
			name:    "machinery missing",
			goMod:   goModWith(alpha2, ""),
			wantErr: errModuleNotRequired,
		},
		{
			name:    "talos missing",
			goMod:   goModWith("", alpha2),
			wantErr: errModuleNotRequired,
		},
	})
}

func TestCheckTalosLockstepReplacements(t *testing.T) {
	t.Parallel()

	runLockstepCases(t, []lockstepCase{
		{
			name: "versioned replace restores lockstep",
			goMod: goModWith(
				alpha2,
				beta1,
				machineryModulePath+" => "+machineryModulePath+" "+alpha2,
			),
			wantErr: nil,
		},
		{
			name: "versioned replace introduces skew",
			goMod: goModWith(
				alpha2,
				alpha2,
				talosModulePath+" "+alpha2+" => "+talosModulePath+" "+beta1,
			),
			wantErr: errTalosMachineryVersion,
		},
		{
			name: "replace pinned to another version is inactive",
			goMod: goModWith(
				alpha2,
				alpha2,
				machineryModulePath+" v1.13.2 => "+machineryModulePath+" v1.13.3",
			),
			wantErr: nil,
		},
		{
			name: "version-specific replace wins over unversioned",
			goMod: goModWith(
				alpha2,
				alpha2,
				machineryModulePath+" => "+machineryModulePath+" "+beta1,
				machineryModulePath+" "+alpha2+" => "+machineryModulePath+" "+alpha2,
			),
			wantErr: nil,
		},
		{
			name:    "local replace cannot be compared",
			goMod:   goModWith(alpha2, alpha2, machineryModulePath+" => ./third_party/machinery"),
			wantErr: errIncomparableReplace,
		},
		{
			name: "fork replace cannot be compared",
			goMod: goModWith(
				alpha2,
				alpha2,
				talosModulePath+" => example.com/fork/talos "+alpha2,
			),
			wantErr: errIncomparableReplace,
		},
	})
}

func runLockstepCases(t *testing.T, testCases []lockstepCase) {
	t.Helper()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := checkTalosLockstep(testCase.goMod)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("checkTalosLockstep() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

// goModWith renders a go.mod requiring talos and machinery at the given versions (an empty
// version leaves that module out) followed by one replace directive per entry.
func goModWith(talos, machinery string, replacements ...string) []byte {
	var builder strings.Builder

	builder.WriteString("module example.com/m\n\nrequire (\n")

	if talos != "" {
		fmt.Fprintf(&builder, "\t%s %s\n", talosModulePath, talos)
	}

	if machinery != "" {
		fmt.Fprintf(&builder, "\t%s %s // indirect\n", machineryModulePath, machinery)
	}

	builder.WriteString(")\n")

	for _, replacement := range replacements {
		fmt.Fprintf(&builder, "\nreplace %s\n", replacement)
	}

	return []byte(builder.String())
}

// checkTalosLockstep reports whether talos and its machinery module resolve to the same
// version in contents, a go.mod file.
//
// The two are released together from one repository under one tag, and talos compiles
// against machinery's config API. machinery is only an indirect requirement here, so any
// dependency that needs a newer machinery (omni/client, image-factory) raises it silently
// while talos stays put, and the build then fails inside talos's own source with an error
// that never names the skew (ksail#6734). This check names it.
func checkTalosLockstep(contents []byte) error {
	file, err := modfile.Parse("go.mod", contents, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod: %w", err)
	}

	talos, err := effectiveVersion(file, talosModulePath)
	if err != nil {
		return err
	}

	machinery, err := effectiveVersion(file, machineryModulePath)
	if err != nil {
		return err
	}

	if talos != machinery {
		return fmt.Errorf(
			"%w: %s is %s but %s is %s. They are released together and talos compiles "+
				"against machinery's config API, so move both to one version, e.g. "+
				"`go get %s@<version> %s@<version>` (ksail#6734)",
			errTalosMachineryVersion,
			talosModulePath, talos, machineryModulePath, machinery,
			talosModulePath, machineryModulePath,
		)
	}

	return nil
}

// effectiveVersion returns the version of modulePath that the build uses: the required
// version, or the version of a replacement that applies to it. A replacement with no
// version (a local directory) or pointing at another module cannot be compared, so it is
// reported instead of guessed.
func effectiveVersion(file *modfile.File, modulePath string) (string, error) {
	required := ""

	for _, requirement := range file.Require {
		if requirement.Mod.Path == modulePath {
			required = requirement.Mod.Version

			break
		}
	}

	if required == "" {
		return "", fmt.Errorf("%w: %s", errModuleNotRequired, modulePath)
	}

	replacement := applicableReplacement(file, modulePath, required)
	if replacement == nil {
		return required, nil
	}

	if replacement.New.Path != modulePath || replacement.New.Version == "" {
		return "", fmt.Errorf(
			"%w: %s => %s %s; state here which version it must match",
			errIncomparableReplace,
			modulePath, replacement.New.Path, replacement.New.Version,
		)
	}

	return replacement.New.Version, nil
}

// applicableReplacement returns the replace directive the go command applies to
// modulePath at version: one naming that exact version wins over one naming none.
func applicableReplacement(file *modfile.File, modulePath, version string) *modfile.Replace {
	var unversioned *modfile.Replace

	for _, replacement := range file.Replace {
		if replacement.Old.Path != modulePath {
			continue
		}

		switch replacement.Old.Version {
		case version:
			return replacement
		case "":
			unversioned = replacement
		}
	}

	return unversioned
}

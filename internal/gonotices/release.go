package gonotices

import (
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// Release binds a recipient payload to its admitted tag, commit and target.
type Release struct{ Version, Source, Target string }

// Layout is one declared physical release archive and its recipient locations.
type Layout struct{ Archive, Format, Binary, Prefix string }

const gitSourceBytes = 20

// ReleaseLayout rejects undeclared artifacts and targets.
func ReleaseLayout(release Release) (Layout, error) {
	if !validRelease(release) {
		return Layout{}, ErrIncomplete
	}

	version := strings.TrimPrefix(release.Version, "v")
	parts := strings.Split(release.Target, "-")

	layout := Layout{Format: "tar.gz", Binary: "ksail", Prefix: "GoNotices/"}
	if parts[1] == "windows" {
		layout.Format, layout.Binary = "zip", "ksail.exe"
	}

	project := "ksail"
	if parts[0] == "desktop" {
		project, layout.Binary = "ksail-desktop", "ksail-desktop"
		if parts[1] == "windows" {
			layout.Binary = "desktop/ksail-desktop.exe"
		}

		if parts[1] == "darwin" {
			project, layout.Format = "KSail", "zip"
			layout.Binary, layout.Prefix = "KSail.app/Contents/MacOS/ksail-desktop", "KSail.app/Contents/Resources/GoNotices/"
		}
	}

	layout.Archive = fmt.Sprintf(
		"%s_%s_%s_%s.%s",
		project,
		version,
		parts[1],
		parts[2],
		layout.Format,
	)

	return layout, nil
}

// Binding creates the strict six-line recipient record shared with install.sh.
func Binding(release Release, payload Payload) ([]byte, error) {
	if !validRelease(release) {
		return nil, ErrIncomplete
	}

	metadata, err := validatePayload(release.Target, payload)
	if err != nil || metadata.Toolchain == nil || metadata.Toolchain.Version == "" ||
		!sha256Hex(metadata.Toolchain.LicenseSHA256) ||
		len(metadata.Toolchain.Packages) == 0 ||
		len(metadata.Toolchain.Licenses) == 0 ||
		len(metadata.Toolchain.Materials) == 0 ||
		len(metadata.Toolchain.Files) == 0 {
		return nil, ErrIncomplete
	}

	return fmt.Appendf(
		nil,
		"KSAIL-GO-NOTICES-1\nversion=%s\nsource=%s\ntarget=%s\nnotices_sha256=%s\nmanifest_sha256=%s\n",
		release.Version,
		release.Source,
		release.Target,
		digest(payload.Text),
		digest(payload.Manifest),
	), nil
}

func validRelease(release Release) bool {
	source, err := hex.DecodeString(release.Source)

	return validTarget(release.Target) && semver.IsValid(release.Version) &&
		semver.Canonical(release.Version) == release.Version &&
		err == nil &&
		len(source) == gitSourceBytes &&
		strings.ToLower(release.Source) == release.Source &&
		release.Source != strings.Repeat("0", hex.EncodedLen(gitSourceBytes))
}

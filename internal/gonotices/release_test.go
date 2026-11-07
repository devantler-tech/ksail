package gonotices_test

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestReleaseBindingUsesExactSixOrderedLines(t *testing.T) {
	t.Parallel()
	payload := releasePayloadFixture(t)
	release := gonotices.Release{
		Version: "v7.206.99",
		Source:  strings.Repeat("a", 40),
		Target:  "cli-linux-amd64",
	}
	data, err := gonotices.Binding(release, payload)
	require.NoError(t, err)

	hash := func(data []byte) string {
		sum := sha256.Sum256(data)

		return hex.EncodeToString(sum[:])
	}
	require.Equal(
		t,
		"KSAIL-GO-NOTICES-1\nversion=v7.206.99\nsource="+release.Source+"\ntarget=cli-linux-amd64\nnotices_sha256="+hash(
			payload.Text,
		)+"\nmanifest_sha256="+hash(
			payload.Manifest,
		)+"\n",
		string(data),
	)
}

func TestReleaseBindingRefusesMalformedAndUnboundInputs(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"version", "newline", "source", "zero-source", "target", "other-target", "empty-text", "tamper",
		"missing-toolchain",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			payload := releasePayloadFixture(t)
			release := gonotices.Release{
				Version: "v7.206.99",
				Source:  strings.Repeat("a", 40),
				Target:  "cli-linux-amd64",
			}

			alterReleaseBinding(t, &payload, &release, scenario)

			_, err := gonotices.Binding(release, payload)
			require.ErrorIs(t, err, gonotices.ErrIncomplete)
		})
	}
}

func alterReleaseBinding(
	t *testing.T,
	payload *gonotices.Payload,
	release *gonotices.Release,
	scenario string,
) {
	t.Helper()

	switch scenario {
	case "version":
		release.Version = "latest"
	case "newline":
		release.Version += "\nsource=another"
	case "source":
		release.Source = strings.Repeat("Z", 40)
	case "zero-source":
		release.Source = strings.Repeat("0", 40)
	case "target":
		release.Target = "cli-freebsd-amd64"
	case "other-target":
		release.Target = "cli-windows-amd64"
	case "empty-text":
		payload.Text = nil
	case "tamper":
		payload.Text = append(payload.Text, []byte("unchecked replacement")...)
	case "missing-toolchain":
		input, _ := noticeInputFixture(t)

		var err error

		*payload, err = gonotices.Build(input)
		require.NoError(t, err)
	}
}

func TestReleaseLayoutsCoverExactlyTheEightDeclaredAssets(t *testing.T) {
	t.Parallel()

	cases := releaseLayoutFixture()

	names := make([]string, 0, len(cases))
	for target, expected := range cases {
		names = append(names, target)
		actual, err := gonotices.ReleaseLayout(
			gonotices.Release{
				Version: "v7.206.99",
				Source:  strings.Repeat("a", 40),
				Target:  target,
			},
		)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}

	require.NoError(t, gonotices.ValidateTargets(names))
}

func releasePayloadFixture(t *testing.T) gonotices.Payload {
	t.Helper()

	return releasePayloadForTargetFixture(t, "cli-linux-amd64")
}

func releasePayloadForTargetFixture(t *testing.T, target string) gonotices.Payload {
	t.Helper()
	input, _ := noticeInputFixture(t)
	input.Target = target
	goRoot := filepath.Join(input.Root, "toolchain")
	writeNoticeFixture(
		t,
		goRoot,
		"LICENSE",
		"Copyright 2009 The Go Authors.\nBSD fixture conditions and disclaimer.\n",
	)
	writeNoticeFixture(t, goRoot, "VERSION", "go1.27.3\n")
	writeNoticeFixture(
		t,
		goRoot,
		"src/runtime/runtime.go",
		"// Copyright 2025 Runtime Authors\npackage runtime\n",
	)
	input.Toolchain = &gonotices.Toolchain{
		Version:     "go1.27.3",
		Directory:   goRoot,
		LicenseFile: filepath.Join(goRoot, "LICENSE"),
		Licenses:    []string{"BSD-3-Clause"},
		Packages: []gonotices.ToolchainPackage{
			{
				ImportPath: "runtime",
				Directory:  filepath.Join(goRoot, "src/runtime"),
				GoFiles:    []string{"runtime.go"},
			},
		},
	}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)

	return payload
}

func releaseLayoutFixture() map[string]gonotices.Layout {
	return map[string]gonotices.Layout{
		"cli-darwin-arm64": {
			Archive: "ksail_7.206.99_darwin_arm64.tar.gz",
			Format:  "tar.gz",
			Binary:  "ksail",
			Prefix:  "GoNotices/",
		},
		"cli-linux-amd64": {
			Archive: "ksail_7.206.99_linux_amd64.tar.gz",
			Format:  "tar.gz",
			Binary:  "ksail",
			Prefix:  "GoNotices/",
		},
		"cli-linux-arm64": {
			Archive: "ksail_7.206.99_linux_arm64.tar.gz",
			Format:  "tar.gz",
			Binary:  "ksail",
			Prefix:  "GoNotices/",
		},
		"cli-windows-amd64": {
			Archive: "ksail_7.206.99_windows_amd64.zip",
			Format:  "zip",
			Binary:  "ksail.exe",
			Prefix:  "GoNotices/",
		},
		"cli-windows-arm64": {
			Archive: "ksail_7.206.99_windows_arm64.zip",
			Format:  "zip",
			Binary:  "ksail.exe",
			Prefix:  "GoNotices/",
		},
		"desktop-darwin-arm64": {
			Archive: "KSail_7.206.99_darwin_arm64.zip",
			Format:  "zip",
			Binary:  "KSail.app/Contents/MacOS/ksail-desktop",
			Prefix:  "KSail.app/Contents/Resources/GoNotices/",
		},
		"desktop-linux-amd64": {
			Archive: "ksail-desktop_7.206.99_linux_amd64.tar.gz",
			Format:  "tar.gz",
			Binary:  "ksail-desktop",
			Prefix:  "GoNotices/",
		},
		"desktop-windows-amd64": {
			Archive: "ksail-desktop_7.206.99_windows_amd64.zip",
			Format:  "zip",
			Binary:  "desktop/ksail-desktop.exe",
			Prefix:  "GoNotices/",
		},
	}
}

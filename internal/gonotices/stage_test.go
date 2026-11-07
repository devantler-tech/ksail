package gonotices_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestStageRetainsExactPayloadAndReleaseBinding(t *testing.T) {
	t.Parallel()
	input, actual, release, payload := stageInputFixture(t)
	directory, err := gonotices.Stage(input.Root, release, actual)
	require.NoError(t, err)
	require.Equal(t, ".cache/go-notices/cli-linux-amd64/GoNotices", directory)

	expectedBinding, err := gonotices.Binding(release, payload)
	require.NoError(t, err)

	for name, expected := range map[string][]byte{
		"THIRD_PARTY_NOTICES.txt": payload.Text,
		"manifest.json":           payload.Manifest,
		"release.txt":             expectedBinding,
	} {
		actual := readNoticeFixture(t, input.Root, filepath.Join(directory, name))
		require.Equal(t, expected, actual)
	}
}

func TestPrepareRefusesChangedActualBuildInputs(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"go.mod", "go.sum", "policy", "materials", "local-source", "toolchain-version", "toolchain-license",
		"missing-material", "linked-stage", "unexpected-stage-member",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			input, actual, release, _ := stageInputFixture(t)

			alterStageInputs(t, &input, &actual, scenario)

			_, err := gonotices.Stage(input.Root, release, actual)
			require.ErrorIs(t, err, gonotices.ErrIncomplete)
		})
	}
}

func alterStageInputs(
	t *testing.T,
	input *gonotices.Input,
	actual *gonotices.Toolchain,
	scenario string,
) {
	t.Helper()

	switch scenario {
	case "go.mod", "go.sum":
		writeNoticeFixture(t, input.Root, scenario, "changed dependency inputs\n")
	case "policy":
		writeNoticeFixture(
			t,
			input.Root,
			".github/go-license-policy.json",
			"{\"changed\":true}\n",
		)
	case "materials":
		writeNoticeFixture(
			t,
			input.Root,
			".github/go-notice-materials.json",
			"{\"changed\":true}\n",
		)
	case "local-source":
		writeNoticeFixture(
			t,
			input.Packages[0].Module.Replace.Directory,
			"source.go",
			"package component\nfunc Changed() {}\n",
		)
	case "toolchain-version":
		actual.Version = "go1.27.4"
	case "toolchain-license":
		writeNoticeFixture(
			t,
			actual.Directory,
			"LICENSE",
			"changed actual compiler license\n",
		)
	case "missing-material":
		removeStageNoticeFixture(t, input)
	case "linked-stage":
		require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(input.Root, ".cache")))
	case "unexpected-stage-member":
		writeNoticeFixture(
			t,
			input.Root,
			".cache/go-notices/cli-linux-amd64/GoNotices/private-note.txt",
			"PRIVATE_CORRESPONDENCE must not enter an archive\n",
		)
	}
}

func removeStageNoticeFixture(t *testing.T, input *gonotices.Input) {
	t.Helper()

	owned, err := os.OpenRoot(input.Root)
	require.NoError(t, err)

	defer func() { require.NoError(t, owned.Close()) }()

	require.NoError(
		t,
		owned.Remove(filepath.Join("notices/go", input.Target, "THIRD_PARTY_NOTICES.txt")),
	)
}

func stageInputFixture(
	t *testing.T,
) (gonotices.Input, gonotices.Toolchain, gonotices.Release, gonotices.Payload) {
	t.Helper()
	input, _ := noticeInputFixture(t)
	goRoot := filepath.Join(input.Root, "toolchain")
	writeNoticeFixture(
		t,
		goRoot,
		"LICENSE",
		"Copyright 2009 The Go Authors.\nBSD fixture conditions.\n",
	)
	writeNoticeFixture(t, goRoot, "VERSION", "go1.27.3\n")
	writeNoticeFixture(
		t,
		goRoot,
		"src/runtime/runtime.go",
		"// Copyright 2025 Runtime Authors\npackage runtime\n",
	)
	actual := gonotices.Toolchain{
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
	input.Toolchain = &actual
	payloads := map[string]gonotices.Payload{}

	for _, target := range []string{
		"cli-darwin-arm64", "cli-linux-amd64", "cli-linux-arm64", "cli-windows-amd64", "cli-windows-arm64",
		"desktop-darwin-arm64", "desktop-linux-amd64", "desktop-windows-amd64",
	} {
		variant := input
		variant.Target = target
		payload, buildErr := gonotices.Build(variant)
		require.NoError(t, buildErr)
		require.NoError(t, gonotices.Write(input.Root, target, payload))
		payloads[target] = payload
	}

	require.NoError(t, gonotices.WriteReceipt(input.Root, payloads))
	payload := payloads[input.Target]

	return input, actual, gonotices.Release{
		Version: "v7.206.99",
		Source:  strings.Repeat("a", 40),
		Target:  input.Target,
	}, payload
}

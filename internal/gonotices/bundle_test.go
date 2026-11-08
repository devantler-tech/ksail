package gonotices_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/sumdb/dirhash"
)

func TestBuildPreservesSelectedLicenseCopyrightAndNestedNotice(t *testing.T) {
	t.Parallel()

	root, directory := selectedNestedNoticeFixture(t)

	payload, err := gonotices.Build(gonotices.Input{
		Root: root, Target: "cli-linux-amd64",
		Packages: []gonotices.Package{{
			ImportPath:  "example.com/component/selected",
			Directory:   filepath.Join(directory, "selected"),
			LicenseFile: filepath.Join(directory, "LICENSE"),
			GoFiles:     []string{"source.go"},
			Raw:         []string{"MIT"}, Effective: []string{"MIT"},
			Module: gonotices.Module{
				Path: "example.com/component", Version: "v1.2.3",
				Replace: &gonotices.Module{Directory: directory, Path: "./third_party/example"},
			},
		}},
	})
	require.NoError(t, err)

	for _, notice := range []string{
		"Copyright 2024 Example Authors", "Selected nested Apache attribution: Example Contributor.",
		"Copyright 2025 Nested Contributor.", "Preserve this source notice.",
	} {
		require.Contains(t, string(payload.Text), notice)
	}

	require.NotContains(t, string(payload.Text), "UNLINKED NOTICE MUST NOT BE COPIED")
	require.NotContains(t, string(payload.Text), root)
	require.NotContains(t, string(payload.Manifest), root)

	var manifest map[string]any
	require.NoError(t, json.Unmarshal(payload.Manifest, &manifest))
	require.Equal(t, "cli-linux-amd64", manifest["target"])
	require.Contains(t, string(payload.Manifest), "third_party/example")
	require.Contains(t, string(payload.Manifest), "example.com/component/selected")
	require.NotContains(t, string(payload.Manifest), "LOFT-ADMIN-APIS-KSAIL-001")
}

func TestBuildAuthenticatesActualReplacementAndProvidesMPLSourceAccess(t *testing.T) {
	t.Parallel()

	input, directory, checksum := mplReplacementFixture(t)

	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(
		t,
		string(payload.Text),
		"https://proxy.golang.org/github.com/example/fork/@v/v2.0.0.zip",
	)
	require.Contains(t, string(payload.Text), checksum)
	require.Contains(t, string(payload.Text), "MPL-2.0")
	require.Contains(t, string(payload.Manifest), "example.com/component")
	require.Contains(t, string(payload.Manifest), "github.com/example/fork")
	require.NotContains(t, string(payload.Manifest), directory)

	writeNoticeFixture(
		t,
		directory,
		"source.go",
		"// Undeclared source modification\npackage component\n",
	)

	_, err = gonotices.Build(input)
	require.ErrorIs(t, err, gonotices.ErrIncomplete)
}

func TestBuildReadsOnlyLeadingCopyrightBeforeLargeGoLiteral(t *testing.T) {
	t.Parallel()

	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"source.go",
		"/*\nCopyright 2025 Font Authors.\npackage this_is_inside_a_comment\n"+
			"Reserved Font Name Example.\n*/\npackage component\nvar font = `"+strings.Repeat(
			"FONT_BODY",
			1<<18,
		)+"`\n",
	)

	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Reserved Font Name Example.")
	require.NotContains(t, string(payload.Text), "FONT_BODY")
	require.Less(t, len(payload.Text), 5000)
}

func TestBuildPreservesCopyrightCommentGroupWithoutCopyingPackageDocumentation(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"source.go",
		"// Copyright 2025 Selected Authors\n// Keep this license condition.\n\n"+
			"// Package component contains unrelated API documentation.\n"+
			"// UNRELATED DOCUMENTATION MUST NOT ENTER NOTICES\npackage component\n",
	)

	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Keep this license condition.")
	require.NotContains(t, string(payload.Text), "UNRELATED DOCUMENTATION MUST NOT ENTER NOTICES")
}

func TestBuildBindsLocalReplacementSourceBeyondItsLicenseHeader(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	before, err := gonotices.Build(input)
	require.NoError(t, err)
	writeNoticeFixture(
		t,
		directory,
		"source.go",
		"// Copyright 2025 Selected Authors\npackage component\nfunc Changed() {}\n",
	)

	after, err := gonotices.Build(input)
	require.NoError(t, err)
	require.NotEqual(
		t,
		string(before.Manifest),
		string(after.Manifest),
		"a local replacement must bind actual modified source, not only unchanged declarations",
	)
}

func TestBuildDeduplicatesOnlyIdenticalTextWithoutLosingPackageAssociations(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"second/source.go",
		"// Copyright 2025 Selected Authors\npackage second\n",
	)

	second := input.Packages[0]
	second.ImportPath += "/second"
	second.Directory = filepath.Join(directory, "second")
	input.Packages = append(input.Packages, second)
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Equal(
		t,
		1,
		strings.Count(string(payload.Text), "MIT\nCopyright 2025 Selected Authors\n"),
	)
	require.Contains(t, string(payload.Manifest), "example.com/component/second")
	require.Contains(t, string(payload.Manifest), "second/source.go")
	require.Contains(t, string(payload.Manifest), "source.go")
}

func TestBuildRetainsNoticesAlongSelectedEmbeddedAssetPaths(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(t, directory, "assets/font.ttf", "font bytes are not recipient notice text")
	writeNoticeFixture(
		t,
		directory,
		"assets/NOTICE",
		"Copyright 2025 Font Authors.\nReserved Font Name Example.\nOFL-1.1 font terms remain distinct.\n",
	)

	input.Packages[0].EmbedFiles = []string{"assets/font.ttf"}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Reserved Font Name Example.")
	require.Contains(t, string(payload.Manifest), "assets/NOTICE")
	require.Contains(t, string(payload.Manifest), "assets/font.ttf")
	require.NotContains(t, string(payload.Text), "font bytes are not recipient notice text")
}

func TestBuildKeepsCompiledComponentScopeSeparateFromSourceAccess(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"LICENSE",
		"Upstream component copyright and unchanged upstream terms.\n",
	)
	checksum, err := dirhash.HashDir(
		directory,
		"github.com/loft-sh/admin-apis@v0.0.0-20260721223200-58c89e54604e",
		dirhash.Hash1,
	)
	require.NoError(t, err)

	input.Packages[0].ImportPath = "github.com/loft-sh/admin-apis/example"
	input.Packages[0].Module = gonotices.Module{
		Path:      "github.com/loft-sh/admin-apis",
		Version:   "v0.0.0-20260721223200-58c89e54604e",
		Sum:       checksum,
		Directory: directory,
	}
	input.Packages[0].Raw = []string{"Unknown"}
	input.Packages[0].Effective = []string{"LicenseRef-Loft-KSail-Binary-Permission"}
	input.Packages[0].UpstreamTermsURL = "https://github.com/loft-sh/admin-apis/blob/58c89e54604e/LICENSE"
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(
		t,
		string(payload.Text),
		"permission record for compilation into KSail binaries",
	)
	require.Contains(t, string(payload.Text), "no general source grant")
	require.NotContains(t, string(payload.Text), "admin-apis/@v/")
	require.NotContains(t, string(payload.Manifest), "admin-apis/@v/")
	require.NotContains(t, string(payload.Text), "LOFT-ADMIN-APIS-KSAIL-001")
}

func TestCollectorKeepsEachTargetFileSetAndIsolatesCachedText(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"source.go",
		"// Copyright 2025 Unix Authors\npackage component\n",
	)
	writeNoticeFixture(
		t,
		directory,
		"windows.go",
		"// Copyright 2025 Windows Authors\npackage component\n",
	)

	collector := new(gonotices.Collector)
	unix, err := collector.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(unix.Text), "Copyright 2025 Unix Authors")
	require.NotContains(t, string(unix.Text), "Copyright 2025 Windows Authors")

	for index := range unix.Text {
		unix.Text[index] = 'X'
	}

	input.Target = "cli-windows-amd64"
	input.Packages[0].GoFiles = []string{"windows.go"}
	windows, err := collector.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(windows.Text), "Copyright 2025 Windows Authors")
	require.NotContains(t, string(windows.Text), "Copyright 2025 Unix Authors")
	require.Contains(t, string(windows.Manifest), "windows.go")

	input.Target = "cli-linux-amd64"
	input.Packages[0].GoFiles = []string{"source.go"}
	repeated, err := collector.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(repeated.Text), "Copyright 2025 Unix Authors")
	require.Contains(t, string(repeated.Text), "MIT\nCopyright 2025 Selected Authors")
}

func TestBuildUsesExactReviewedDeclarationWhenScannerHasNoLicensePath(t *testing.T) {
	t.Parallel()
	input, _ := noticeInputFixture(t)
	input.Packages[0].LicenseFile = ""
	input.Packages[0].EvidenceFiles = []string{"LICENSE"}
	input.Packages[0].Raw = []string{"Unknown"}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Copyright 2025 Selected Authors")
	require.Contains(t, string(payload.Manifest), "Unknown")

	input.Packages[0].EvidenceFiles = nil
	_, err = gonotices.Build(input)
	require.ErrorIs(t, err, gonotices.ErrIncomplete)
}

func TestBuildCountsANestedLicenseSelectedAsEvidenceOnce(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(t, directory, "licenses/LICENSE", "MIT\nCopyright 2025 Nested Authors\n")
	// The scanner path is native and the reviewed evidence path is slash-form.
	// On Windows they differ as strings but name one file.
	input.Packages[0].LicenseFile = filepath.Join(directory, "licenses", "LICENSE")
	input.Packages[0].EvidenceFiles = []string{"licenses/LICENSE"}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(payload.Text), "Copyright 2025 Nested Authors"))
	require.NoError(t, gonotices.Write(input.Root, input.Target, payload))
}

func TestBuildCollectsSupplementalSelectedAncestorCopyingTerms(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"COPYING",
		"Copyright 2025 Font Authors.\nReserved Font Name Example.\nSIL OPEN FONT LICENSE Version 1.1\n",
	)
	// The approved effective font terms do not make the scanner-selected MIT
	// file carry the separate copyrighted font declaration automatically.
	input.Packages[0].Effective = []string{"MIT", "OFL-1.1"}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Reserved Font Name Example.")
	require.Contains(t, string(payload.Manifest), "COPYING")
}

func TestBuildIgnoresUnlinkedToolDirectoryNamedCopyright(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	// golang.org/x/tools has an ordinary copyright/ Go-tool directory beside
	// its actual LICENSE. A directory name is not a text declaration.
	writeNoticeFixture(t, directory, "copyright/tool.go", "package main\n// UNLINKED TOOL SOURCE\n")

	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.NotContains(t, string(payload.Text), "UNLINKED TOOL SOURCE")
	require.NotContains(t, string(payload.Manifest), "copyright/tool.go")
}

func TestBuildRetainsSelectedModuleAssemblyCopyright(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"asm.s",
		"// Copyright 2025 Module Assembly Authors\n// Keep these assembly conditions.\n"+
			"#include \"textflag.h\"\nTEXT fake(SB), $0\n",
	)

	input.Packages[0].AssemblyFiles = []string{"asm.s"}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Module Assembly Authors")
	require.Contains(t, string(payload.Manifest), "asm.s")
	require.NotContains(t, string(payload.Text), "TEXT fake")
}

func TestBuildRetainsHeadersOfSelectedCompiledCgoInputs(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"bridge.cc",
		"/* Copyright 2025 Bridge Authors\n * Preserve this selected source condition.\n */\n"+
			"#include \"bridge.h\"\nint implementation = 1;\n",
	)

	input.Packages[0].OtherFiles = []string{"bridge.cc"}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Bridge Authors")
	require.Contains(t, string(payload.Manifest), "bridge.cc")
	require.NotContains(t, string(payload.Text), "int implementation")
}

func TestBuildRefusesUnsafeOrIncompleteSourceMaterial(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"module-root-link", "license-link", "embedded-traversal", "private-marker",
		"missing-license", "duplicate-package", "unknown-effective", "unknown-target",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input, directory := noticeInputFixture(t)

			switch name {
			case "module-root-link":
				outside := t.TempDir()
				writeNoticeFixture(t, outside, "LICENSE", "MIT\nCopyright 2025 Outside Authors\n")
				writeNoticeFixture(t, outside, "source.go", "package component\n")

				linked := filepath.Join(input.Root, "third_party", "linked")
				require.NoError(t, os.Symlink(outside, linked))
				input.Packages[0].Module.Replace.Directory = linked
				input.Packages[0].Directory = linked
				input.Packages[0].LicenseFile = filepath.Join(linked, "LICENSE")
			case "license-link":
				outsideRoot := t.TempDir()
				outside := filepath.Join(outsideRoot, "LICENSE")
				writeNoticeFixture(
					t,
					outsideRoot,
					"LICENSE",
					"MIT\nCopyright Private Outside Author\n",
				)
				require.NoError(t, os.Remove(filepath.Join(directory, "LICENSE")))
				require.NoError(t, os.Symlink(outside, filepath.Join(directory, "LICENSE")))
			case "embedded-traversal":
				input.Packages[0].EmbedFiles = []string{"../outside-font.ttf"}
			case "private-marker":
				writeNoticeFixture(
					t,
					directory,
					"LICENSE",
					"MIT\nCopyright 2025 Authors\n/private/tmp/private-correspondence/never-publish\n",
				)
			case "missing-license":
				require.NoError(t, os.Remove(filepath.Join(directory, "LICENSE")))
			case "duplicate-package":
				input.Packages = append(input.Packages, input.Packages[0])
			case "unknown-effective":
				input.Packages[0].Effective = []string{"Unknown"}
			case "unknown-target":
				input.Target = "cli-future-amd64"
			}

			_, err := gonotices.Build(input)
			require.ErrorIs(t, err, gonotices.ErrIncomplete)
		})
	}
}

func noticeInputFixture(t *testing.T) (gonotices.Input, string) {
	t.Helper()
	root := t.TempDir()
	writeNoticeFixture(t, root, "go.mod", "module github.com/devantler-tech/ksail/v7\n")
	writeNoticeFixture(t, root, "go.sum", "selected public source metadata\n")
	writeNoticeFixture(t, root, ".github/go-license-policy.json", "{\"fixture\":true}\n")
	writeNoticeFixture(t, root, ".github/go-notice-materials.json", "{\"schema\":1,\"files\":[]}\n")
	directory := filepath.Join(root, "third_party", "example")
	writeNoticeFixture(t, directory, "LICENSE", "MIT\nCopyright 2025 Selected Authors\n")
	writeNoticeFixture(
		t,
		directory,
		"source.go",
		"// Copyright 2025 Selected Authors\npackage component\n",
	)

	return gonotices.Input{Root: root, Target: "cli-linux-amd64", Packages: []gonotices.Package{{
		ImportPath: "example.com/component", Directory: directory,
		LicenseFile: filepath.Join(directory, "LICENSE"), GoFiles: []string{"source.go"},
		Raw: []string{"MIT"}, Effective: []string{"MIT"},
		Module: gonotices.Module{
			Path: "example.com/component", Version: "v1.2.3",
			Replace: &gonotices.Module{Path: "./third_party/example", Directory: directory},
		},
	}}}, directory
}

func writeNoticeFixture(t *testing.T, root, name, contents string) {
	t.Helper()

	path := filepath.Join(root, name)
	require.True(t, strings.HasPrefix(path, root+string(filepath.Separator)))
	require.NoError(t, os.MkdirAll(root, 0o700))
	owned, err := os.OpenRoot(root)
	require.NoError(t, err)

	defer func() { require.NoError(t, owned.Close()) }()

	require.NoError(t, owned.MkdirAll(filepath.Dir(name), 0o700))
	require.NoError(t, owned.WriteFile(name, []byte(contents), 0o600))
}

func readNoticeFixture(t *testing.T, root, name string) []byte {
	t.Helper()

	owned, err := os.OpenRoot(root)
	require.NoError(t, err)

	defer func() { require.NoError(t, owned.Close()) }()

	data, err := owned.ReadFile(name)
	require.NoError(t, err)

	return data
}

func selectedNestedNoticeFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeNoticeFixture(t, root, "go.mod", "module github.com/devantler-tech/ksail/v7\n")
	writeNoticeFixture(t, root, "go.sum", "fixture module checksum inputs\n")
	writeNoticeFixture(t, root, ".github/go-license-policy.json", "{\"fixture\":true}\n")
	writeNoticeFixture(t, root, ".github/go-notice-materials.json", "{\"schema\":1,\"files\":[]}\n")
	directory := filepath.Join(root, "third_party", "example")
	writeNoticeFixture(
		t,
		directory,
		"LICENSE",
		"MIT\nCopyright 2024 Example Authors\nPermission notice from the actual selected source.\n",
	)
	writeNoticeFixture(
		t,
		directory,
		"selected/NOTICE",
		"Selected nested Apache attribution: Example Contributor.\n",
	)
	writeNoticeFixture(
		t,
		directory,
		"selected/source.go",
		"// Copyright 2025 Nested Contributor.\n// Preserve this source notice.\npackage selected\n",
	)
	writeNoticeFixture(t, directory, "unlinked/NOTICE", "UNLINKED NOTICE MUST NOT BE COPIED\n")

	return root, directory
}

func mplReplacementFixture(t *testing.T) (gonotices.Input, string, string) {
	t.Helper()
	root := t.TempDir()
	writeNoticeFixture(t, root, "go.mod", "module github.com/devantler-tech/ksail/v7\n")
	writeNoticeFixture(t, root, "go.sum", "selected public source metadata\n")
	writeNoticeFixture(t, root, ".github/go-license-policy.json", "{\"fixture\":true}\n")
	writeNoticeFixture(t, root, ".github/go-notice-materials.json", "{\"schema\":1,\"files\":[]}\n")
	directory := t.TempDir()
	writeNoticeFixture(
		t,
		directory,
		"LICENSE",
		"Mozilla Public License Version 2.0\nCopyright 2025 Fork Authors\n",
	)
	writeNoticeFixture(
		t,
		directory,
		"source.go",
		"// Copyright 2025 Fork Authors\npackage component\n",
	)
	checksum, err := dirhash.HashDir(directory, "github.com/example/fork@v2.0.0", dirhash.Hash1)
	require.NoError(t, err)

	input := gonotices.Input{
		Root:   root,
		Target: "desktop-linux-amd64",
		Packages: []gonotices.Package{{
			ImportPath: "example.com/component", Directory: directory,
			LicenseFile: filepath.Join(directory, "LICENSE"), GoFiles: []string{"source.go"},
			Raw: []string{"MPL-2.0"}, Effective: []string{"MPL-2.0"},
			Module: gonotices.Module{
				Path:    "example.com/component",
				Version: "v1.2.3",
				Sum:     "h1:declared-not-selected",
				Replace: &gonotices.Module{
					Path:      "github.com/example/fork",
					Version:   "v2.0.0",
					Sum:       checksum,
					Directory: directory,
				},
			},
		}},
	}

	return input, directory, checksum
}

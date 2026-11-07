package gonotices_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestBuildIncludesActualToolchainMaterialWithoutSyntheticModuleIdentity(t *testing.T) {
	t.Parallel()
	input, _ := noticeInputFixture(t)
	goRoot := filepath.Join(input.Root, "test-toolchain")
	writeNoticeFixture(
		t,
		goRoot,
		"LICENSE",
		"Copyright 2009 The Go Authors.\n"+
			"Redistribution in binary form must retain these fixture conditions and disclaimer.\n",
	)
	writeNoticeFixture(t, goRoot, "VERSION", "go1.27.3\n")
	writeNoticeFixture(
		t,
		goRoot,
		"src/runtime/runtime.go",
		"// Copyright 2025 Runtime Authors\npackage runtime\n",
	)
	writeNoticeFixture(
		t,
		goRoot,
		"src/vendor/example/NOTICE",
		"Copyright 2025 Selected Standard Vendor Authors\n",
	)
	writeNoticeFixture(t, goRoot, "src/vendor/example/example.go", "package example\n")
	input.Toolchain = &gonotices.Toolchain{
		Version: "go1.27.3", Directory: goRoot,
		LicenseFile: filepath.Join(goRoot, "LICENSE"), Licenses: []string{"BSD-3-Clause"},
		Packages: []gonotices.ToolchainPackage{
			{
				ImportPath: "runtime",
				Directory:  filepath.Join(goRoot, "src/runtime"),
				GoFiles:    []string{"runtime.go"},
			},
			{
				ImportPath: "vendor/example",
				Directory:  filepath.Join(goRoot, "src/vendor/example"),
				GoFiles:    []string{"example.go"},
			},
		},
	}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Copyright 2009 The Go Authors.")
	require.Contains(t, string(payload.Text), "Copyright 2025 Runtime Authors")
	require.Contains(t, string(payload.Text), "Selected Standard Vendor Authors")
	require.NotContains(t, string(payload.Manifest), goRoot)

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload.Manifest, &decoded))

	var record map[string]any
	require.NoError(t, json.Unmarshal(decoded["toolchain"], &record))
	require.Equal(t, "go1.27.3", record["version"])
	require.NotContains(t, record, "module")
	require.NotContains(t, record, "checksum")
}

func TestBuildIncludesSelectedRuntimeAssemblyCopyright(t *testing.T) {
	t.Parallel()
	input, _ := noticeInputFixture(t)
	goRoot := filepath.Join(input.Root, "test-toolchain")
	writeNoticeFixture(
		t,
		goRoot,
		"LICENSE",
		"Copyright 2009 The Go Authors.\nBSD fixture license.\n",
	)
	writeNoticeFixture(t, goRoot, "VERSION", "go1.27.3\n")
	writeNoticeFixture(
		t,
		goRoot,
		"src/runtime/asm.s",
		"// Copyright 2025 Assembly Authors.\n// Preserve these assembly conditions.\n"+
			"#include \"textflag.h\"\nTEXT fake(SB), $0\n",
	)
	input.Toolchain = &gonotices.Toolchain{
		Version:     "go1.27.3",
		Directory:   goRoot,
		LicenseFile: filepath.Join(goRoot, "LICENSE"),
		Licenses:    []string{"BSD-3-Clause"},
		Packages: []gonotices.ToolchainPackage{
			{
				ImportPath:    "runtime",
				Directory:     filepath.Join(goRoot, "src/runtime"),
				AssemblyFiles: []string{"asm.s"},
			},
		},
	}
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), "Assembly Authors")
	require.Contains(t, string(payload.Text), "Preserve these assembly conditions.")
	require.NotContains(t, string(payload.Text), "TEXT fake")
}

func TestBuildUsesTheSameToolchainEvidenceAcrossEquivalentInstallations(t *testing.T) {
	t.Parallel()

	input, _ := noticeInputFixture(t)
	standard := filepath.Join(input.Root, "downloaded-go")
	packaged := filepath.Join(input.Root, "packaged-go", "1.27.3", "libexec")
	license := "Copyright 2009 The Go Authors.\nBSD fixture conditions and disclaimer.\n"
	writeNoticeFixture(t, standard, "LICENSE", license)
	writeNoticeFixture(t, filepath.Dir(packaged), "LICENSE", license)

	first := toolchainInstallationPayloadFixture(
		t,
		input,
		standard,
		filepath.Join(standard, "LICENSE"),
	)
	second := toolchainInstallationPayloadFixture(
		t,
		input,
		packaged,
		filepath.Join(filepath.Dir(packaged), "LICENSE"),
	)
	require.Equal(t, string(first.Text), string(second.Text))
	require.Equal(t, string(first.Manifest), string(second.Manifest))
}

func toolchainInstallationPayloadFixture(
	t *testing.T,
	input gonotices.Input,
	goRoot, license string,
) gonotices.Payload {
	t.Helper()

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
		LicenseFile: license,
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

package gonotices_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/sumdb/dirhash"
)

const upstreamMITFixture = "MIT License\n\nCopyright (c) 2017 Upstream Authors\n\nPermission is hereby granted.\n"

func TestBuildEmitsReviewedUpstreamLicenseForExactSelectedModule(t *testing.T) {
	t.Parallel()

	input, policy := upstreamLicenseFixture(t)

	texts, err := policy.UpstreamTexts(input.Root, input.Packages[0], []string{"Apache-2.0", "MIT"})
	require.NoError(t, err)
	require.Len(t, texts, 1)

	input.Packages[0].UpstreamTexts = texts

	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Contains(t, string(payload.Text), upstreamMITFixture)
	require.Contains(t, string(payload.Text), "(upstream-license)")
	require.Contains(t, string(payload.Text), "Files: example.com/upstream@v1.0.3/LICENSE")
	require.Contains(t, string(payload.Text), "Apache fork LICENSE fixture")
	require.Contains(t, string(payload.Manifest), `"kind":"upstream-license"`)

	require.NoError(
		t,
		policy.VerifySupplementsUsed(map[string]gonotices.Payload{input.Target: payload}),
	)

	stored := t.TempDir()
	require.NoError(t, gonotices.Write(stored, input.Target, payload))
	require.NoError(t, gonotices.Verify(stored, input.Target, payload))
}

func TestUpstreamLicenseFailsClosedWhenSelectionOrTextChanges(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"checksum-mismatch", "wrong-version", "wrong-sum", "replaced", "unapproved-terms",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			input, policy := upstreamLicenseFixture(t)
			pkg := input.Packages[0]
			allowed := []string{"Apache-2.0", "MIT"}

			switch scenario {
			case "checksum-mismatch":
				writeNoticeFixture(
					t,
					input.Root,
					".github/go-notice-supplements/example.com/upstream@v1.0.3/LICENSE",
					upstreamMITFixture+"tampered\n",
				)
			case "wrong-version":
				pkg.Module.Version = "v1.10.0"
			case "wrong-sum":
				pkg.Module.Sum = "h1:another-source"
			case "replaced":
				pkg.Module.Replace = &gonotices.Module{
					Path: "./third_party/fork", Directory: pkg.Module.Directory,
				}
			case "unapproved-terms":
				allowed = []string{"Apache-2.0"}
			}

			_, err := policy.UpstreamTexts(input.Root, pkg, allowed)
			require.ErrorIs(t, err, gonotices.ErrIncomplete)
		})
	}
}

func TestUpstreamLicenseIgnoresOtherModules(t *testing.T) {
	t.Parallel()

	input, policy := upstreamLicenseFixture(t)
	pkg := input.Packages[0]
	pkg.Module.Path = "example.com/unrelated"
	pkg.ImportPath = "example.com/unrelated"

	texts, err := policy.UpstreamTexts(input.Root, pkg, []string{"Apache-2.0", "MIT"})
	require.NoError(t, err)
	require.Empty(t, texts)
}

func TestVerifySupplementsUsedRefusesUnselectedSupplement(t *testing.T) {
	t.Parallel()

	input, policy := upstreamLicenseFixture(t)

	// The module is selected, but no text was attached: the supplement was never emitted.
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.ErrorIs(
		t,
		policy.VerifySupplementsUsed(map[string]gonotices.Payload{input.Target: payload}),
		gonotices.ErrIncomplete,
	)

	// A policy without supplements needs no emission.
	require.NoError(t, gonotices.MaterialPolicy{Schema: 1}.VerifySupplementsUsed(
		map[string]gonotices.Payload{input.Target: payload},
	))
}

func TestBuildRefusesMalformedUpstreamText(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"invalid-locator", "collides-with-module-file", "private"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			input, _ := upstreamLicenseFixture(t)
			text := gonotices.UpstreamText{
				Path:  "example.com/upstream@v1.0.3/LICENSE",
				Text:  []byte(upstreamMITFixture),
				Terms: []string{"MIT"},
			}

			switch scenario {
			case "invalid-locator":
				text.Path = "../LICENSE"
			case "collides-with-module-file":
				writeNoticeFixture(
					t,
					input.Packages[0].Module.Directory,
					"example.com/upstream@v1.0.3/LICENSE",
					"module-owned file\n",
				)
				input = rehashUpstreamFixture(t, input)
				input.Packages[0].EvidenceFiles = []string{"example.com/upstream@v1.0.3/LICENSE"}
			case "private":
				text.Text = []byte("/Users/someone/never-publish\n")
			}

			input.Packages[0].UpstreamTexts = []gonotices.UpstreamText{text}

			_, err := gonotices.Build(input)
			require.ErrorIs(t, err, gonotices.ErrIncomplete)
		})
	}
}

func TestLoadMaterialPolicyVerifiesUpstreamLicenseSupplements(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"valid", "checksum-mismatch", "missing-text", "invalid-locator", "unknown-terms",
		"duplicate", "missing-reason",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			input, policy := upstreamLicenseFixture(t)

			switch scenario {
			case "checksum-mismatch":
				policy.Supplements[0].SHA256 = strings.Repeat("0", 64)
			case "missing-text":
				policy.Supplements[0].Upstream = "example.com/upstream@v1.0.3/COPYING"
			case "invalid-locator":
				policy.Supplements[0].Upstream = "example.com/upstream/LICENSE"
			case "unknown-terms":
				policy.Supplements[0].Terms = []string{"Unknown"}
			case "duplicate":
				policy.Supplements = append(policy.Supplements, policy.Supplements[0])
			case "missing-reason":
				policy.Supplements[0].Reason = ""
			}

			data, err := json.Marshal(policy)
			require.NoError(t, err)
			writeNoticeFixture(t, input.Root, ".github/go-notice-materials.json", string(data)+"\n")

			loaded, err := gonotices.LoadMaterialPolicy(input.Root)
			if scenario == "valid" {
				require.NoError(t, err)
				require.Equal(t, policy, loaded)
			} else {
				require.ErrorIs(t, err, gonotices.ErrIncomplete)
			}
		})
	}
}

func upstreamLicenseFixture(t *testing.T) (gonotices.Input, gonotices.MaterialPolicy) {
	t.Helper()

	root := upstreamLicenseRootFixture(t)
	directory := t.TempDir()
	writeNoticeFixture(t, directory, "LICENSE", "Apache fork LICENSE fixture\n")
	writeNoticeFixture(
		t,
		directory,
		"source.go",
		"// Copyright 2024 Fork Authors\n\n// Forked from: example.com/upstream\n// MIT License\n"+
			"// Copyright (c) 2017 Upstream Authors\n\npackage fork\n",
	)

	input := gonotices.Input{
		Root:   root,
		Target: "cli-linux-amd64",
		Packages: []gonotices.Package{
			{
				ImportPath:  "example.com/fork",
				Directory:   directory,
				LicenseFile: filepath.Join(directory, "LICENSE"),
				GoFiles:     []string{"source.go"},
				Raw:         []string{"Apache-2.0"},
				Effective:   []string{"Apache-2.0"},
				Module: gonotices.Module{
					Path:      "example.com/fork",
					Version:   "v1.9.0",
					Directory: directory,
				},
			},
		},
	}
	input = rehashUpstreamFixture(t, input)

	sum := sha256.Sum256([]byte(upstreamMITFixture))

	return input, gonotices.MaterialPolicy{
		Schema: 1,
		Files:  []gonotices.MaterialRule{},
		Supplements: []gonotices.UpstreamLicense{
			{
				Module:   "example.com/fork",
				Version:  "v1.9.0",
				Sum:      input.Packages[0].Module.Sum,
				Upstream: "example.com/upstream@v1.0.3/LICENSE",
				SHA256:   hex.EncodeToString(sum[:]),
				Terms:    []string{"MIT"},
				Reason:   "The fork's sources name the MIT upstream but the module ships only its own license.",
			},
		},
	}
}

func upstreamLicenseRootFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeNoticeFixture(t, root, "go.mod", "module github.com/devantler-tech/ksail/v7\n")
	writeNoticeFixture(t, root, "go.sum", "selected public source metadata\n")
	writeNoticeFixture(t, root, ".github/go-license-policy.json", "{\"fixture\":true}\n")
	writeNoticeFixture(t, root, ".github/go-notice-materials.json", "{\"schema\":1,\"files\":[]}\n")
	writeNoticeFixture(
		t,
		root,
		".github/go-notice-supplements/example.com/upstream@v1.0.3/LICENSE",
		upstreamMITFixture,
	)

	return root
}

func rehashUpstreamFixture(t *testing.T, input gonotices.Input) gonotices.Input {
	t.Helper()

	checksum, err := dirhash.HashDir(
		input.Packages[0].Module.Directory,
		"example.com/fork@v1.9.0",
		dirhash.Hash1,
	)
	require.NoError(t, err)

	input.Packages[0].Module.Sum = checksum

	return input
}

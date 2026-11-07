package gonotices_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestVerifyRequiresActualSourceDerivedRecipientFiles(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"complete", "missing", "stale-text", "coherent-self-manifest-tamper", "changed-source", "linked-directory",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input, source := noticeInputFixture(t)
			payload, err := gonotices.Build(input)
			require.NoError(t, err)

			if name != "missing" {
				require.NoError(t, gonotices.Write(input.Root, input.Target, payload))
			}

			output := filepath.Join(input.Root, "notices", "go", input.Target)

			switch name {
			case "stale-text":
				require.NoError(
					t,
					os.WriteFile(
						filepath.Join(output, "THIRD_PARTY_NOTICES.txt"),
						[]byte("SPDX labels only; no copyrighted material"),
						0o600,
					),
				)
			case "coherent-self-manifest-tamper":
				// Even a coherent alternate manifest/text pair cannot replace the
				// independently derived selected-source expectation.
				writeCoherentAlternateNoticeFixture(t, output)
			case "changed-source":
				writeNoticeFixture(
					t,
					source,
					"source.go",
					"// Copyright 2025 Selected Authors\npackage component\nfunc Changed() {}\n",
				)

				payload, err = gonotices.Build(input)
				require.NoError(t, err)
			case "linked-directory":
				outside := filepath.Join(t.TempDir(), "copied")
				require.NoError(t, os.Rename(output, outside))
				require.NoError(t, os.Symlink(outside, output))
			}

			err = gonotices.Verify(input.Root, input.Target, payload)
			if name == "complete" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, gonotices.ErrIncomplete)
			}
		})
	}
}

func writeCoherentAlternateNoticeFixture(t *testing.T, output string) {
	t.Helper()

	input, source := noticeInputFixture(t)
	writeNoticeFixture(t, source, "LICENSE", "MIT\nCopyright 2025 Someone Else\n")

	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(output, "manifest.json"), payload.Manifest, 0o600),
	)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(output, "THIRD_PARTY_NOTICES.txt"), payload.Text, 0o600),
	)
}

func TestWriteRefusesWrongTargetOrMalformedPayloadBindings(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"wrong-target", "duplicate-key", "unknown-key", "trailing-json", "changed-text"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input, _ := noticeInputFixture(t)
			payload, err := gonotices.Build(input)
			require.NoError(t, err)

			switch name {
			case "wrong-target":
				payload.Manifest = []byte(
					strings.ReplaceAll(
						string(payload.Manifest),
						"cli-linux-amd64",
						"desktop-linux-amd64",
					),
				)
			case "duplicate-key":
				trimmed := strings.TrimSpace(string(payload.Manifest))
				payload.Manifest = []byte(
					trimmed[:len(trimmed)-1] + ",\"target\":\"cli-linux-amd64\"}\n",
				)
			case "unknown-key":
				payload.Manifest = []byte("{\"unknown\":true," + string(payload.Manifest[1:]))
			case "trailing-json":
				payload.Manifest = append(payload.Manifest, []byte("{}")...)
			case "changed-text":
				payload.Text = append(payload.Text, []byte("coherently unverified new material")...)
			}

			require.ErrorIs(
				t,
				gonotices.Write(input.Root, input.Target, payload),
				gonotices.ErrIncomplete,
			)
		})
	}
}

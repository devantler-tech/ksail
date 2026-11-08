package gonotices_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

type recipientArchive struct {
	target, name, format, binary, prefix string
}

type recipientEntry struct {
	name string
	data []byte
	mode fs.FileMode
}

type recipientExtra struct {
	name, comment string
	size          int64
}

type recipientMutation struct {
	name  string
	alter func([]recipientEntry) []recipientEntry
}

func TestArchiveVerifiesTheActualEightReleaseLayouts(t *testing.T) {
	t.Parallel()

	for _, fixture := range recipientArchiveFixtures() {
		t.Run(fixture.target, func(t *testing.T) {
			t.Parallel()

			payload := releasePayloadForTargetFixture(t, fixture.target)
			release := recipientRelease(fixture.target)
			entries := recipientEntries(fixture, release, payload)
			archive := writeRecipientArchive(t, fixture, entries)

			require.NoError(t, gonotices.VerifyArchive(archive, release, payload))
		})
	}
}

func TestArchiveRefusesAnAbsentArtifact(t *testing.T) {
	t.Parallel()

	payload := releasePayloadFixture(t)
	err := gonotices.VerifyArchive(filepath.Join(t.TempDir(), "ksail_7.206.99_linux_amd64.tar.gz"),
		recipientRelease("cli-linux-amd64"), payload)
	require.ErrorIs(t, err, gonotices.ErrIncomplete)
}

func TestArchiveRefusesMissingAlteredAndUnsafeMembers(t *testing.T) {
	t.Parallel()

	for _, fixture := range []recipientArchive{recipientArchiveFixtures()[1], recipientArchiveFixtures()[3]} {
		t.Run(fixture.format, func(t *testing.T) {
			t.Parallel()

			payload := releasePayloadForTargetFixture(t, fixture.target)

			mutations := append(recipientMaterialMutations(), recipientPathMutations()...)
			mutations = append(mutations, recipientWindowsAliases()...)

			for _, scenario := range mutations {
				t.Run(scenario.name, func(t *testing.T) {
					t.Parallel()

					requireRecipientArchiveRefusal(t, fixture, payload, scenario.alter)
				})
			}
		})
	}
}

func recipientMaterialMutations() []recipientMutation {
	return []recipientMutation{
		{
			"missing notices",
			func(v []recipientEntry) []recipientEntry { return slices.Delete(v, 2, 3) },
		},
		{"missing binary", func(v []recipientEntry) []recipientEntry { return v[1:] }},
		{"duplicate notices", func(v []recipientEntry) []recipientEntry { return append(v, v[2]) }},
		{"duplicate binary", func(v []recipientEntry) []recipientEntry { return append(v, v[0]) }},
		{
			"duplicate unrelated file",
			func(v []recipientEntry) []recipientEntry { return append(v, v[5]) },
		},
		{"case alias of required material", func(v []recipientEntry) []recipientEntry {
			return append(
				v,
				recipientEntry{strings.ToUpper(v[3].name), []byte("replacement"), 0o644},
			)
		}},
		{"altered notices", func(v []recipientEntry) []recipientEntry {
			v[2].data = []byte("replacement")

			return v
		}},
		{"altered manifest", func(v []recipientEntry) []recipientEntry {
			v[3].data = []byte("{}")

			return v
		}},
		{"wrong release", func(v []recipientEntry) []recipientEntry {
			v[4].data = bytes.Replace(v[4].data, []byte("v7.206.99"), []byte("v7.206.98"), 1)

			return v
		}},
		{"empty binary", func(v []recipientEntry) []recipientEntry {
			v[0].data = nil

			return v
		}},
	}
}

func recipientPathMutations() []recipientMutation {
	return []recipientMutation{
		{"traversal", func(v []recipientEntry) []recipientEntry {
			return append(v, recipientEntry{"../outside", []byte("untrusted"), 0o644})
		}},
		{"absolute path", func(v []recipientEntry) []recipientEntry {
			return append(v, recipientEntry{"/outside", []byte("untrusted"), 0o644})
		}},
		{"Windows traversal", func(v []recipientEntry) []recipientEntry {
			return append(v, recipientEntry{`..\outside`, []byte("untrusted"), 0o644})
		}},
		{"Windows drive", func(v []recipientEntry) []recipientEntry {
			return append(v, recipientEntry{"C:/outside", []byte("untrusted"), 0o644})
		}},
		{"file replaces parent directory", func(v []recipientEntry) []recipientEntry {
			v[1] = recipientEntry{"GoNotices", []byte("not a directory"), 0o644}

			return v
		}},
		{"parent file follows its children", func(v []recipientEntry) []recipientEntry {
			v = slices.Delete(v, 1, 2)

			return append(v, recipientEntry{"GoNotices", []byte("not a directory"), 0o644})
		}},
		{"symlink", func(v []recipientEntry) []recipientEntry {
			return append(
				v,
				recipientEntry{"outside-link", []byte("../outside"), fs.ModeSymlink | 0o777},
			)
		}},
	}
}

func recipientWindowsAliases() []recipientMutation {
	invalid := []recipientMutation{}
	for _, char := range []string{"?", "*", "<", ">", `"`, "|", "\t"} {
		invalid = append(invalid, recipientMutation{
			"windows-invalid " + fmt.Sprintf("%q", char) + " before required files",
			func(v []recipientEntry) []recipientEntry {
				return append(
					[]recipientEntry{{"extra" + char + ".txt", []byte("extra"), 0o644}},
					v...,
				)
			},
		})
	}

	return append(invalid, []recipientMutation{
		{"trailing dot in leaf", func(v []recipientEntry) []recipientEntry {
			return append(v, recipientEntry{v[3].name + ".", []byte("replacement"), 0o644})
		}},
		{"trailing space in leaf", func(v []recipientEntry) []recipientEntry {
			return append(v, recipientEntry{v[3].name + " ", []byte("replacement"), 0o644})
		}},
		{"trailing dot in parent", func(v []recipientEntry) []recipientEntry {
			return append(
				v,
				recipientEntry{"GoNotices./manifest.json", []byte("replacement"), 0o644},
			)
		}},
		{"trailing space in parent", func(v []recipientEntry) []recipientEntry {
			return append(
				v,
				recipientEntry{"GoNotices /manifest.json", []byte("replacement"), 0o644},
			)
		}},
	}...)
}

func requireRecipientArchiveRefusal(
	t *testing.T,
	fixture recipientArchive,
	payload gonotices.Payload,
	alter func([]recipientEntry) []recipientEntry,
) {
	t.Helper()

	release := recipientRelease(fixture.target)
	entries := alter(recipientEntries(fixture, release, payload))
	archive := writeRecipientArchive(t, fixture, entries)
	err := gonotices.VerifyArchive(archive, release, payload)
	require.ErrorIs(t, err, gonotices.ErrIncomplete)
	require.NotContains(t, err.Error(), filepath.Dir(archive))
}

func TestArchiveBoundsExpandedMembers(t *testing.T) {
	t.Parallel()

	for _, fixture := range []recipientArchive{recipientArchiveFixtures()[1], recipientArchiveFixtures()[3]} {
		t.Run(fixture.format+" oversized member", func(t *testing.T) {
			t.Parallel()

			payload := releasePayloadForTargetFixture(t, fixture.target)
			release := recipientRelease(fixture.target)
			archive := writeRecipientArchive(
				t,
				fixture,
				recipientEntries(fixture, release, payload),
				recipientExtra{name: "oversized", size: (512 << 20) + 1},
			)
			require.ErrorIs(
				t,
				gonotices.VerifyArchive(archive, release, payload),
				gonotices.ErrIncomplete,
			)
		})
	}
}

// TestArchiveBoundsCumulativeZIPExpansion admits each member individually but
// rejects the archive once the members together expand beyond 2 GiB.
func TestArchiveBoundsCumulativeZIPExpansion(t *testing.T) {
	t.Parallel()

	const (
		memberBytes = 512 << 20
		members     = 5
	)

	fixture := recipientArchiveFixtures()[3]
	payload := releasePayloadForTargetFixture(t, fixture.target)
	release := recipientRelease(fixture.target)

	// Compress one permitted member once and reuse the raw stream, so the
	// fixture stays small without writing 2.5 GiB through the compressor.
	var compressed bytes.Buffer

	deflater, err := flate.NewWriter(&compressed, flate.BestSpeed)
	require.NoError(t, err)

	checksum := crc32.NewIEEE()
	_, err = io.CopyN(io.MultiWriter(deflater, checksum), zeroRecipientBytes{}, memberBytes)
	require.NoError(t, err)
	require.NoError(t, deflater.Close())

	directory := t.TempDir()
	file, err := recipientFixtureRoot(t, directory).Create(fixture.name)
	require.NoError(t, err)

	writer := zip.NewWriter(file)
	writeRecipientZIPEntries(t, writer, recipientEntries(fixture, release, payload))

	compressedSize := uint64(compressed.Len()) //nolint:gosec // G115: lengths are non-negative.

	for index := range members {
		header := &zip.FileHeader{
			Name:               fmt.Sprintf("expanded-%d", index),
			Method:             zip.Deflate,
			CRC32:              checksum.Sum32(),
			CompressedSize64:   compressedSize,
			UncompressedSize64: memberBytes,
		}
		header.SetMode(0o644)
		member, createErr := writer.CreateRaw(header)
		require.NoError(t, createErr)

		_, writeErr := member.Write(compressed.Bytes())
		require.NoError(t, writeErr)
	}

	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())
	require.ErrorIs(
		t,
		gonotices.VerifyArchive(filepath.Join(directory, fixture.name), release, payload),
		gonotices.ErrIncomplete,
	)
}

func TestArchiveBoundsIndexSizeAndMemberCount(t *testing.T) {
	t.Parallel()

	t.Run("member count", func(t *testing.T) {
		t.Parallel()

		fixture := recipientArchiveFixtures()[3]
		payload := releasePayloadForTargetFixture(t, fixture.target)
		release := recipientRelease(fixture.target)
		entries := recipientEntries(fixture, release, payload)

		for index := range 10_001 {
			entries = append(entries, recipientEntry{fmt.Sprintf("extra/%05d", index), nil, 0o644})
		}

		archive := writeRecipientArchive(t, fixture, entries)
		require.ErrorIs(
			t,
			gonotices.VerifyArchive(archive, release, payload),
			gonotices.ErrIncomplete,
		)
	})
	t.Run("central directory byte budget", func(t *testing.T) {
		t.Parallel()

		fixture := recipientArchiveFixtures()[3]
		payload := releasePayloadForTargetFixture(t, fixture.target)
		release := recipientRelease(fixture.target)
		extra := make([]recipientExtra, 0, 257)

		for index := range 257 {
			extra = append(
				extra,
				recipientExtra{
					name:    fmt.Sprintf("comment/%05d", index),
					comment: strings.Repeat("a", 32<<10),
				},
			)
		}

		archive := writeRecipientArchive(
			t,
			fixture,
			recipientEntries(fixture, release, payload),
			extra...)
		require.ErrorIs(
			t,
			gonotices.VerifyArchive(archive, release, payload),
			gonotices.ErrIncomplete,
		)
	})
}

func TestArchiveRefusesCoherentReplacementOfItsOwnEvidence(t *testing.T) {
	t.Parallel()

	fixture := recipientArchiveFixtures()[1]
	expected := releasePayloadForTargetFixture(t, fixture.target)
	forged := expected
	forged.Text = append(
		bytes.Clone(expected.Text),
		[]byte("replacement not derived from source\n")...)
	forgedDigest := []byte(fmt.Sprintf(`"textSha256":"%x"`, sha256.Sum256(forged.Text)))
	forged.Manifest = bytes.Replace(expected.Manifest,
		[]byte(fmt.Sprintf(`"textSha256":"%x"`, sha256.Sum256(expected.Text))),
		forgedDigest, 1)
	require.True(t, bytes.Contains(forged.Manifest, forgedDigest),
		"the forged manifest must name the forged text digest")

	release := recipientRelease(fixture.target)
	archive := writeRecipientArchive(t, fixture, recipientEntries(fixture, release, forged))

	require.ErrorIs(t, gonotices.VerifyArchive(archive, release, expected), gonotices.ErrIncomplete)
}

func TestArchiveRefusesCorruptionAndConcatenatedGzipStreams(t *testing.T) {
	t.Parallel()

	fixture := recipientArchiveFixtures()[1]
	payload := releasePayloadForTargetFixture(t, fixture.target)
	release := recipientRelease(fixture.target)

	for _, scenario := range []string{"checksum", "truncated trailer", "second stream"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			archive := writeRecipientArchive(
				t,
				fixture,
				recipientEntries(fixture, release, payload),
			)
			root := recipientFixtureRoot(t, filepath.Dir(archive))
			data, err := root.ReadFile(filepath.Base(archive))
			require.NoError(t, err)

			switch scenario {
			case "checksum":
				data[len(data)-8] ^= 0x01
			case "truncated trailer":
				data = data[:len(data)-8]
			case "second stream":
				data = append(data, bytes.Clone(data)...)
			}

			require.NoError(t, root.WriteFile(filepath.Base(archive), data, 0o600))
			require.ErrorIs(
				t,
				gonotices.VerifyArchive(archive, release, payload),
				gonotices.ErrIncomplete,
			)
		})
	}
}

func recipientArchiveFixtures() []recipientArchive {
	return []recipientArchive{
		{"cli-darwin-arm64", "ksail_7.206.99_darwin_arm64.tar.gz", "tar.gz", "ksail", "GoNotices/"},
		{"cli-linux-amd64", "ksail_7.206.99_linux_amd64.tar.gz", "tar.gz", "ksail", "GoNotices/"},
		{"cli-linux-arm64", "ksail_7.206.99_linux_arm64.tar.gz", "tar.gz", "ksail", "GoNotices/"},
		{"cli-windows-amd64", "ksail_7.206.99_windows_amd64.zip", "zip", "ksail.exe", "GoNotices/"},
		{"cli-windows-arm64", "ksail_7.206.99_windows_arm64.zip", "zip", "ksail.exe", "GoNotices/"},
		{
			"desktop-darwin-arm64", "KSail_7.206.99_darwin_arm64.zip", "zip",
			"KSail.app/Contents/MacOS/ksail-desktop", "KSail.app/Contents/Resources/GoNotices/",
		},
		{
			"desktop-linux-amd64",
			"ksail-desktop_7.206.99_linux_amd64.tar.gz",
			"tar.gz",
			"ksail-desktop",
			"GoNotices/",
		},
		{
			"desktop-windows-amd64", "ksail-desktop_7.206.99_windows_amd64.zip", "zip",
			"desktop/ksail-desktop.exe", "GoNotices/",
		},
	}
}

func recipientRelease(target string) gonotices.Release {
	return gonotices.Release{Version: "v7.206.99", Source: strings.Repeat("b", 40), Target: target}
}

func recipientEntries(
	fixture recipientArchive,
	release gonotices.Release,
	payload gonotices.Payload,
) []recipientEntry {
	// The expected archive record is constructed independently of Binding and Layout.
	binding := fmt.Appendf(
		nil,
		"KSAIL-GO-NOTICES-1\nversion=%s\nsource=%s\ntarget=%s\nnotices_sha256=%x\nmanifest_sha256=%x\n",
		release.Version,
		release.Source,
		release.Target,
		sha256.Sum256(payload.Text),
		sha256.Sum256(payload.Manifest),
	)

	return []recipientEntry{
		{fixture.binary, []byte("test-only executable contents"), 0o755},
		{fixture.prefix, nil, fs.ModeDir | 0o755},
		{fixture.prefix + "THIRD_PARTY_NOTICES.txt", payload.Text, 0o644},
		{fixture.prefix + "manifest.json", payload.Manifest, 0o644},
		{fixture.prefix + "release.txt", binding, 0o644},
		{"README.md", []byte("ordinary accompanying documentation\n"), 0o644},
	}
}

func writeRecipientArchive(
	t *testing.T,
	fixture recipientArchive,
	entries []recipientEntry,
	extra ...recipientExtra,
) string {
	t.Helper()

	directory := t.TempDir()
	name := filepath.Join(directory, fixture.name)
	root := recipientFixtureRoot(t, directory)
	file, err := root.Create(fixture.name)
	require.NoError(t, err)

	if fixture.format == "zip" {
		writeRecipientZIPFixture(t, file, entries, extra)
	} else {
		writeRecipientTarFixture(t, file, entries, extra)
	}

	require.NoError(t, file.Close())

	return name
}

func writeRecipientZIPFixture(
	t *testing.T,
	file *os.File,
	entries []recipientEntry,
	extra []recipientExtra,
) {
	t.Helper()

	writer := zip.NewWriter(file)
	writeRecipientZIPEntries(t, writer, entries)

	for _, entry := range extra {
		header := &zip.FileHeader{Name: entry.name, Comment: entry.comment, Method: zip.Deflate}
		header.SetMode(0o644)
		member, createErr := writer.CreateHeader(header)
		require.NoError(t, createErr)

		_, writeErr := io.CopyN(member, zeroRecipientBytes{}, entry.size)
		require.NoError(t, writeErr)
	}

	require.NoError(t, writer.Close())
}

func writeRecipientZIPEntries(t *testing.T, writer *zip.Writer, entries []recipientEntry) {
	t.Helper()

	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		member, createErr := writer.CreateHeader(header)
		require.NoError(t, createErr)

		_, writeErr := member.Write(entry.data)
		require.NoError(t, writeErr)
	}
}

func writeRecipientTarFixture(
	t *testing.T,
	file *os.File,
	entries []recipientEntry,
	extra []recipientExtra,
) {
	t.Helper()

	compressed := gzip.NewWriter(file)
	writer := tar.NewWriter(compressed)

	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name,
			Mode: int64(entry.mode.Perm()),
			Size: int64(len(entry.data)),
		}
		if entry.mode.IsDir() {
			header.Typeflag = tar.TypeDir
		} else if entry.mode&fs.ModeSymlink != 0 {
			header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, string(entry.data), 0
		}

		require.NoError(t, writer.WriteHeader(header))

		if header.Typeflag != tar.TypeSymlink {
			_, writeErr := writer.Write(entry.data)
			require.NoError(t, writeErr)
		}
	}

	for _, entry := range extra {
		require.NoError(
			t,
			writer.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: entry.size}),
		)
		_, writeErr := io.CopyN(writer, zeroRecipientBytes{}, entry.size)
		require.NoError(t, writeErr)
	}

	require.NoError(t, writer.Close())
	require.NoError(t, compressed.Close())
}

// zeroRecipientBytes creates valid compressed oversized members without a large allocation.
type zeroRecipientBytes struct{}

func (zeroRecipientBytes) Read(data []byte) (int, error) {
	clear(data)

	return len(data), nil
}

func recipientFixtureRoot(t *testing.T, directory string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	return root
}

package gonotices_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestCommandStagesOnlyCommitBoundRecipientMaterial(t *testing.T) {
	t.Parallel()
	root, release := commandInputFixture(t)

	var output bytes.Buffer
	require.NoError(
		t,
		gonotices.Run(
			root,
			[]string{"stage", release.Target, release.Version, release.Source},
			&output,
		),
	)
	data := readNoticeFixture(
		t,
		root,
		filepath.Join(".cache/go-notices", release.Target, "GoNotices/release.txt"),
	)
	require.Contains(t, string(data), "source="+release.Source+"\n")
	require.NotContains(t, output.String(), root)
}

func TestCommandStagesAfterCRLFCheckoutWithIsolatedGitConfig(t *testing.T) {
	t.Parallel()
	root, release := commandInputFixture(t)
	files := []string{".goreleaser.yaml", ".goreleaser.desktop.yaml", ".github/workflows/cd.yaml"}

	for _, name := range files {
		require.NoError(t, os.Remove(filepath.Join(root, filepath.FromSlash(name))))
	}

	checkoutArgs := append(
		[]string{"-c", "core.autocrlf=true", "restore", "--source=HEAD", "--"},
		files...)
	//nolint:gosec // G204: fixed Git arguments and fixture paths.
	runFixtureCommand(t, root, exec.CommandContext(t.Context(), "git", checkoutArgs...))

	// Force Git to compare bytes after checkout rather than reuse the index stat cache.
	staleTime := time.Unix(1_700_000_000, 0)

	for _, name := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.Chtimes(path, staleTime, staleTime))
	}

	args := []string{"stage", release.Target, release.Version, release.Source}

	var output bytes.Buffer
	require.NoError(t, gonotices.Run(root, args, &output))
	require.Contains(t, output.String(), "Verified Go recipient material for "+release.Target)
}

// TestCommandStagesThroughALinkedToolchainRoot covers actions/setup-go on
// Windows, which reports GOROOT through a junction to the D: drive. A symlink
// to the real GOROOT stands in for that junction on every platform.
func TestCommandStagesThroughALinkedToolchainRoot(t *testing.T) {
	root, release := commandInputFixture(t)
	goRoot, _ := fixtureGoEnvironment(t, root)
	linked := filepath.Join(t.TempDir(), "go-link")
	symlinkNoticeFixture(t, goRoot, linked)
	t.Setenv("GOROOT", linked)

	var output bytes.Buffer
	require.NoError(
		t,
		gonotices.Run(
			root,
			[]string{"stage", release.Target, release.Version, release.Source},
			&output,
		),
	)
	require.Contains(t, output.String(), "Verified Go recipient material for "+release.Target)
}

func TestCommandStagesTheClosedCLICohort(t *testing.T) {
	t.Parallel()
	root, release := commandInputFixture(t)
	fixtures := recipientArchiveFixtures()

	var output bytes.Buffer

	for _, operation := range []string{"assets", "verify-all"} {
		require.ErrorIs(
			t,
			gonotices.Run(root, []string{operation, release.Version, release.Source}, &output),
			gonotices.ErrIncomplete,
		)
	}

	require.Empty(t, output.String())
	require.NoError(
		t,
		gonotices.Run(root, []string{"stage-cli", release.Version, release.Source}, &output),
	)
	require.Len(
		t,
		strings.FieldsFunc(output.String(), func(value rune) bool { return value == '\n' }),
		5,
	)

	for _, fixture := range fixtures[:5] {
		data := readNoticeFixture(
			t,
			root,
			filepath.Join(".cache/go-notices", fixture.target, "GoNotices/release.txt"),
		)
		require.Contains(t, string(data), "source="+release.Source+"\n")
		require.Contains(t, string(data), "target="+fixture.target+"\n")
	}

	writeCommandArchiveFixtures(t, root, release, "dist", fixtures[:5])
	output.Reset()
	require.NoError(
		t,
		gonotices.Run(root, []string{"verify-cli", release.Version, release.Source}, &output),
	)
	require.Equal(t, 5, strings.Count(output.String(), "Verified Go recipient material for "))
	require.NotContains(t, output.String(), root)
}

func TestWindowsCheckoutPreservesExactNoticeAndInputBytes(t *testing.T) {
	const filesPerTarget = 2

	t.Parallel()
	root, _ := commandInputFixture(t)
	destination := t.TempDir()
	checkout := exec.CommandContext(t.Context(), "git", "-c", "core.autocrlf=true",
		"checkout-index", "--all", "--force")
	checkout.Env = []string{"GIT_WORK_TREE=" + destination}
	runFixtureCommand(t, root, checkout)

	inputs := []string{
		"go.mod", "go.sum", ".github/go-license-policy.json", ".github/go-notice-materials.json",
		".github/go-notice-receipts.json",
	}
	fixtures := recipientArchiveFixtures()
	paths := make([]string, 0, len(inputs)+filesPerTarget*len(fixtures))
	paths = append(paths, inputs...)

	for _, fixture := range fixtures {
		paths = append(
			paths,
			filepath.Join("notices/go", fixture.target, "THIRD_PARTY_NOTICES.txt"),
			filepath.Join("notices/go", fixture.target, "manifest.json"),
		)
	}

	for _, path := range paths {
		require.Equal(
			t,
			readNoticeFixture(t, root, path),
			readNoticeFixture(t, destination, path),
			path,
		)
	}
}

func writeCommandArchiveFixtures(
	t *testing.T,
	root string,
	release gonotices.Release,
	directory string,
	fixtures []recipientArchive,
) {
	t.Helper()

	for _, fixture := range fixtures {
		payload := gonotices.Payload{
			Text: readNoticeFixture(
				t,
				root,
				filepath.Join("notices/go", fixture.target, "THIRD_PARTY_NOTICES.txt"),
			),
			Manifest: readNoticeFixture(
				t,
				root,
				filepath.Join("notices/go", fixture.target, "manifest.json"),
			),
		}
		selected := release
		selected.Target = fixture.target
		archive := writeRecipientArchive(t, fixture, recipientEntries(fixture, selected, payload))
		data := readNoticeFixture(t, filepath.Dir(archive), filepath.Base(archive))
		writeNoticeFixture(t, root, filepath.Join(directory, fixture.name), string(data))
	}
}

func TestCommandRejectsUnboundCheckoutAndCoherentPayloadTampering(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"wrong-source", "dirty-input", "coherent-text-and-manifest", "unknown-operation", "unsafe-directory",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			root, release := commandInputFixture(t)
			args := []string{"stage", release.Target, release.Version, release.Source}

			switch scenario {
			case "wrong-source":
				args[3] = strings.Repeat("b", 40)
			case "dirty-input":
				writeNoticeFixture(t, root, "go.sum", "uncommitted source substitution\n")
			case "coherent-text-and-manifest":
				directory := filepath.Join(root, "notices/go", release.Target)
				text := []byte("Coherently replaced material without original copyrighted terms\n")
				writeNoticeFixture(t, directory, "THIRD_PARTY_NOTICES.txt", string(text))
				data := readNoticeFixture(t, directory, "manifest.json")

				var metadata map[string]any
				require.NoError(t, json.Unmarshal(data, &metadata))

				hash := sha256.Sum256(text)
				textSHA256 := hex.EncodeToString(hash[:])
				metadata["textSha256"] = textSHA256
				// Preserve canonical field order by replacing only its fixed-size value.
				var original struct {
					TextSHA256 string `json:"textSha256"`
				}
				require.NoError(t, json.Unmarshal(data, &original))
				data = bytes.Replace(
					data,
					[]byte(original.TextSHA256),
					[]byte(textSHA256),
					1,
				)
				writeNoticeFixture(t, directory, "manifest.json", string(data))
			case "unknown-operation":
				args[0] = "execute"
			case "unsafe-directory":
				args = []string{
					"verify",
					release.Target,
					release.Version,
					release.Source,
					"/private/tmp/elsewhere",
				}
			}

			var output bytes.Buffer
			require.ErrorIs(t, gonotices.Run(root, args, &output), gonotices.ErrIncomplete)
			require.Empty(t, output.String())
		})
	}
}

func commandInputFixture(t *testing.T) (string, gonotices.Release) {
	t.Helper()
	input, actual, release, _ := stageInputFixture(t)

	goRoot, goVersion := fixtureGoEnvironment(t, input.Root)
	licenseRoot := goRoot

	_, inspectErr := os.Stat(filepath.Join(goRoot, "LICENSE"))
	if inspectErr != nil {
		licenseRoot = filepath.Dir(goRoot)
	}

	license := readNoticeFixture(t, licenseRoot, "LICENSE")
	writeNoticeFixture(t, actual.Directory, "LICENSE", string(license))
	actual.Version = goVersion
	writeNoticeFixture(t, actual.Directory, "VERSION", actual.Version+"\n")
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

	for _, file := range []string{".goreleaser.yaml", ".goreleaser.desktop.yaml", ".github/workflows/cd.yaml"} {
		writeNoticeFixture(t, input.Root, file, "source-bound producer fixture\n")
	}

	attributes := readNoticeFixture(t, filepath.Join("..", ".."), ".gitattributes")
	writeNoticeFixture(t, input.Root, ".gitattributes", string(attributes))

	release.Source = initializeNoticeSourceFixture(t, input.Root)

	return input.Root, release
}

func fixtureGoEnvironment(t *testing.T, root string) (string, string) {
	t.Helper()
	output := runFixtureCommand(
		t,
		root,
		exec.CommandContext(t.Context(), "go", "env", "-json", "GOROOT", "GOVERSION"),
	)

	var values map[string]string
	require.NoError(t, json.Unmarshal([]byte(output), &values))
	require.Len(t, values, 2)
	require.True(t, filepath.IsAbs(values["GOROOT"]))
	require.NotEmpty(t, values["GOVERSION"])

	return values["GOROOT"], values["GOVERSION"]
}

func runFixtureCommand(t *testing.T, root string, command *exec.Cmd) string {
	t.Helper()

	command.Dir = root

	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") && !strings.HasPrefix(entry, "GOPROXY=") {
			command.Env = append(command.Env, entry)
		}
	}

	command.Env = append(command.Env, "GOPROXY=off", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))

	return string(output)
}

func initializeNoticeSourceFixture(t *testing.T, root string) string {
	t.Helper()
	runFixtureCommand(
		t,
		root,
		exec.CommandContext(t.Context(), "git", "init", "--quiet", "--template="),
	)
	runFixtureCommand(t, root, exec.CommandContext(t.Context(), "git",
		"add",
		"go.mod",
		"go.sum",
		".gitattributes",
		".github",
		".goreleaser.yaml",
		".goreleaser.desktop.yaml",
		"third_party",
		"notices",
		"toolchain",
	))
	runFixtureCommand(t, root, exec.CommandContext(t.Context(), "git",
		"-c",
		"user.name=Notice Fixture",
		"-c",
		"user.email=notice@example.invalid",
		"-c",
		"commit.gpgsign=false",
		"-c",
		"core.hooksPath=/dev/null",
		"commit",
		"--quiet",
		"-m",
		"fixture",
	))

	return strings.TrimSpace(runFixtureCommand(t, root,
		exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")))
}

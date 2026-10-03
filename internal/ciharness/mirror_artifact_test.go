package ciharness_test

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mirrorArchiveNames() []string {
	return []string{
		"docker.io.tar",
		"ghcr.io.tar",
		"quay.io.tar",
		"registry.k8s.io.tar",
		"ecr-public.aws.com.tar",
	}
}

func mirrorArchiveFixture(t *testing.T) *os.Root {
	t.Helper()
	directory, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, directory.Close()) })

	for _, name := range mirrorArchiveNames() {
		var contents bytes.Buffer

		archive := tar.NewWriter(&contents)
		payload := []byte("validated image data")
		require.NoError(
			t,
			archive.WriteHeader(
				&tar.Header{Name: "image-data", Mode: 0o600, Size: int64(len(payload))},
			),
		)
		_, err = archive.Write(payload)
		require.NoError(t, err)
		require.NoError(t, archive.Close())
		require.NoError(t, directory.WriteFile(name, contents.Bytes(), 0o600))
	}

	return directory
}

func replaceMirrorArchiveWithSymlink(t *testing.T, directory *os.Root, name string) {
	t.Helper()

	other := "docker.io.tar"
	if name == other {
		other = "ghcr.io.tar"
	}

	require.NoError(t, directory.Remove(name))
	require.NoError(t, directory.Symlink(other, name))
}

func runMirrorProducerValidation(t *testing.T, directory *os.Root) (string, error) {
	t.Helper()
	action := readCompositeAction(t, ".github/actions/warm-mirror-cache/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🔍 Verify all mirror volumes exported")
	actionPath, err := filepath.Abs(
		filepath.Join("..", "..", ".github", "actions", "warm-mirror-cache"),
	)
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(
		strings.ReplaceAll(step.Run, "/tmp/mirror-cache", `"$FIXTURE_MIRROR_DIR"`),
	)

	command.Env = append(os.Environ(), "GITHUB_ACTION_PATH="+actionPath, "CACHE_KEY=mirror-fixture",
		"FIXTURE_MIRROR_DIR="+directory.Name())
	output, err := command.CombinedOutput()

	return string(output), err
}

// Exercise the restored-cache decision with controlled successful registry probes.
func runMirrorCacheCompleteness(t *testing.T, directory *os.Root) (string, error) {
	t.Helper()
	action := readCompositeAction(t, ".github/actions/warm-mirror-cache/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🔍 Check if cache is complete")
	actionPath, err := filepath.Abs(
		filepath.Join("..", "..", ".github", "actions", "warm-mirror-cache"),
	)
	require.NoError(t, err)
	bin := t.TempDir()
	writeExecutableStub(t, filepath.Join(bin, "docker"), "#!/bin/sh\nexit 0\n")
	writeExecutableStub(t, filepath.Join(bin, "curl"), "#!/bin/sh\nexit 0\n")
	require.NoError(t, directory.WriteFile("images", nil, 0o600))
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(strings.NewReplacer(
		"/tmp/mirror-cache", `"$FIXTURE_MIRROR_DIR"`,
		"/tmp/all-images.txt", `"$FIXTURE_IMAGES"`,
	).Replace(step.Run))

	command.Env = append(os.Environ(), "GITHUB_ACTION_PATH="+actionPath,
		"CACHE_KEY=mirror-fixture", "GITHUB_RUN_ID=503", "GITHUB_RUN_ATTEMPT=2",
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FIXTURE_MIRROR_DIR="+directory.Name(),
		"FIXTURE_IMAGES="+filepath.Join(directory.Name(), "images"),
		"GITHUB_OUTPUT="+filepath.Join(directory.Name(), "outputs"))
	output, err := command.CombinedOutput()

	return string(output), err
}

func TestMirrorRestoredCacheRepairsInvalidArchives(t *testing.T) {
	t.Parallel()

	for _, name := range mirrorArchiveNames() {
		for _, invalid := range []string{"missing", "empty", "malformed", "symlink"} {
			t.Run(name+"/"+invalid, func(t *testing.T) {
				t.Parallel()
				directory := mirrorArchiveFixture(t)

				switch invalid {
				case "missing":
					require.NoError(t, directory.Remove(name))
				case "empty":
					require.NoError(t, directory.WriteFile(name, nil, 0o600))
				case "malformed":
					require.NoError(t, directory.WriteFile(name, []byte("not an archive"), 0o600))
				case "symlink":
					replaceMirrorArchiveWithSymlink(t, directory, name)
				}

				output, err := runMirrorCacheCompleteness(t, directory)
				require.NoError(t, err, "invalid cached inputs must enter repair: %s", output)

				outputs, readErr := directory.ReadFile("outputs")
				require.NoError(t, readErr)
				assert.Equal(t,
					"complete=false\nrepair-key=mirror-fixture-repair-503-2\n", string(outputs))
			})
		}
	}
}

func TestMirrorRestoredCacheKeepsValidArchives(t *testing.T) {
	t.Parallel()
	directory := mirrorArchiveFixture(t)
	output, err := runMirrorCacheCompleteness(t, directory)
	require.NoError(t, err, output)
	outputs, err := directory.ReadFile("outputs")
	require.NoError(t, err)
	assert.Equal(t, "complete=true\n", string(outputs))

	_, err = directory.Stat("SHA256SUMS")
	assert.ErrorIs(t, err, os.ErrNotExist, "checking a restored cache must not reseal it")
}

func TestMirrorRestoredCacheChecksConsumerPull(t *testing.T) {
	t.Parallel()
	command := exec.CommandContext(
		t.Context(),
		"bash",
		"../../.github/actions/warm-mirror-cache/cache-validation.test.sh",
	)
	output, err := command.CombinedOutput()
	require.NoError(
		t,
		err,
		"the restored-cache fixture must reach the failed consumer pull: %s",
		output,
	)
}

func TestMirrorRegenerationReplacesRejectedSymlinks(t *testing.T) {
	t.Parallel()
	action := readCompositeAction(t, ".github/actions/warm-mirror-cache/action.yaml")
	export := findHarnessStep(t, action.Runs.Steps, "💾 Export mirror volumes")
	seal := findHarnessStep(t, action.Runs.Steps, "🔍 Verify all mirror volumes exported")
	assert.Equal(t, "steps.check-cache.outputs.complete != 'true'", export.If)

	actionPath, err := filepath.Abs(
		filepath.Join("..", "..", ".github", "actions", "warm-mirror-cache"),
	)
	require.NoError(t, err)

	for _, name := range mirrorArchiveNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory := mirrorArchiveFixture(t)
			valid := mirrorArchiveFixture(t)

			replaceMirrorArchiveWithSymlink(t, directory, name)
			bin := t.TempDir()
			writeExecutableStub(t, filepath.Join(bin, "docker"), `#!/bin/bash
set -euo pipefail
while [ "$#" -gt 0 ]; do
  if [ "$1" = tar ]; then
    [ "$2" = -cf ] || exit 1
    name=${3#/backup/}
    cp "$FIXTURE_VALID_TAR" "$FIXTURE_MIRROR_DIR/$name"
    exit 0
  fi
  shift
done
exit 1
`)
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Stdin = strings.NewReader(strings.ReplaceAll(
				export.Run+"\n"+seal.Run, "/tmp/mirror-cache", `"$FIXTURE_MIRROR_DIR"`,
			))

			command.Env = append(os.Environ(), "GITHUB_ACTION_PATH="+actionPath,
				"CACHE_KEY=mirror-repaired", "FIXTURE_MIRROR_DIR="+directory.Name(),
				"FIXTURE_VALID_TAR="+filepath.Join(valid.Name(), "docker.io.tar"),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, runErr := command.CombinedOutput()
			require.NoError(t, runErr, "regenerated archives must seal successfully: %s", output)

			repaired, openErr := os.OpenRoot(directory.Name())
			require.NoError(t, openErr)
			t.Cleanup(func() { require.NoError(t, repaired.Close()) })

			for _, archive := range mirrorArchiveNames() {
				info, statErr := repaired.Lstat(archive)
				require.NoError(t, statErr)
				assert.True(t, info.Mode().IsRegular(), "%s must be a new regular archive", archive)
			}

			key, readErr := repaired.ReadFile("cache-key")
			require.NoError(t, readErr)
			assert.Equal(t, "mirror-repaired\n", string(key))
		})
	}
}

func TestMirrorProducerRejectsIncompleteArtifact(t *testing.T) {
	t.Parallel()

	for _, invalid := range []string{"missing", "empty", "malformed"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			directory := mirrorArchiveFixture(t)

			switch invalid {
			case "missing":
				require.NoError(t, directory.Remove("quay.io.tar"))
			case "empty":
				require.NoError(t, directory.WriteFile("quay.io.tar", nil, 0o600))
			case "malformed":
				require.NoError(
					t,
					directory.WriteFile("quay.io.tar", []byte("not an archive"), 0o600),
				)
			}

			output, err := runMirrorProducerValidation(t, directory)
			require.Error(
				t,
				err,
				"partial or corrupt mirror artifacts must not reach consumers: %s",
				output,
			)

			_, statErr := directory.Stat("SHA256SUMS")
			assert.ErrorIs(
				t,
				statErr,
				os.ErrNotExist,
				"failed validation must not publish a manifest",
			)
		})
	}
}

func TestMirrorProducerSealsCompleteArtifact(t *testing.T) {
	t.Parallel()
	directory := mirrorArchiveFixture(t)
	output, err := runMirrorProducerValidation(t, directory)
	require.NoError(t, err, output)
	key, err := directory.ReadFile("cache-key")
	require.NoError(t, err, "validated artifact must carry its exact producer key")
	assert.Equal(t, "mirror-fixture\n", string(key))
	command := exec.CommandContext(t.Context(), "sha256sum", "--check", "SHA256SUMS")
	command.Dir = directory.Name()
	verification, err := command.CombinedOutput()
	require.NoError(t, err, string(verification))

	for _, name := range mirrorArchiveNames() {
		assert.Contains(t, string(verification), name+": OK")
	}
}

func sealedMirrorFixture(t *testing.T) *os.Root {
	t.Helper()
	directory := mirrorArchiveFixture(t)
	require.NoError(t, directory.WriteFile("cache-key", []byte("mirror-fixture\n"), 0o600))

	var manifest strings.Builder

	for _, name := range append(mirrorArchiveNames(), "cache-key") {
		contents, err := directory.ReadFile(name)
		require.NoError(t, err)
		fmt.Fprintf(&manifest, "%x  %s\n", sha256.Sum256(contents), name)
	}

	require.NoError(t, directory.WriteFile("SHA256SUMS", []byte(manifest.String()), 0o600))

	return directory
}

func runMirrorConsumerValidation(t *testing.T, directory *os.Root, key string) (string, error) {
	t.Helper()
	action := readCompositeAction(t, ".github/actions/restore-mirror-cache/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🔍 Verify producer mirror artifact")
	actionPath, err := filepath.Abs(
		filepath.Join("..", "..", ".github", "actions", "restore-mirror-cache"),
	)
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(
		strings.ReplaceAll(step.Run, "/tmp/mirror-cache", `"$FIXTURE_MIRROR_DIR"`),
	)

	command.Env = append(
		os.Environ(),
		"GITHUB_ACTION_PATH="+actionPath,
		"CACHE_KEY="+key,
		"FIXTURE_MIRROR_DIR="+directory.Name(),
		"GITHUB_OUTPUT="+filepath.Join(directory.Name(), "outputs"),
	)
	output, err := command.CombinedOutput()

	return string(output), err
}

func TestMirrorConsumerAcceptsSealedProducerArtifact(t *testing.T) {
	t.Parallel()
	directory := sealedMirrorFixture(t)
	output, err := runMirrorConsumerValidation(t, directory, "mirror-fixture")
	require.NoError(t, err, output)
	assert.Contains(t, output, "Validated all five mirror archives")

	outputs, err := directory.ReadFile("outputs")
	require.NoError(t, err)
	assert.Equal(t, "cache-hit=true\n", string(outputs))
}

func TestMirrorConsumerRejectsChangedOrUnboundArtifact(t *testing.T) {
	t.Parallel()

	for _, invalid := range []string{"wrong-key", "changed-archive", "partial-manifest", "missing-metadata"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			directory := sealedMirrorFixture(t)
			key := "mirror-fixture"
			want := "checksum mismatch"

			switch invalid {
			case "wrong-key":
				key = "another-producer"
				want = "producer key"
			case "changed-archive":
				contents, err := directory.ReadFile("quay.io.tar")
				require.NoError(t, err)

				contents[512] = 'X' // Valid tar with different image content.
				require.NoError(t, directory.WriteFile("quay.io.tar", contents, 0o600))
			case "partial-manifest":
				contents, err := directory.ReadFile("SHA256SUMS")
				require.NoError(t, err)

				first, _, _ := strings.Cut(string(contents), "\n")
				require.NoError(t, directory.WriteFile("SHA256SUMS", []byte(first+"\n"), 0o600))
			case "missing-metadata":
				require.NoError(t, directory.Remove("SHA256SUMS"))

				want = "Missing mirror artifact metadata"
			}

			output, err := runMirrorConsumerValidation(t, directory, key)
			require.Error(t, err, output)
			assert.Contains(t, output, want)
			assert.NotContains(t, output, "Validated all five mirror archives")
		})
	}
}

func TestMirrorArtifactIdentityGuardsDownload(t *testing.T) {
	t.Parallel()
	action := readCompositeAction(t, ".github/actions/restore-mirror-cache/action.yaml")

	step := findHarnessStep(t, action.Runs.Steps, "🔍 Require producer artifact")
	for _, testCase := range []struct {
		name, id, key, required string
		wantError               bool
	}{
		{name: "consumer-only-rerun", id: "123456", key: "mirror-fixture", required: "true"},
		{name: "standalone-cache", required: "false"},
		{name: "missing-producer", key: "mirror-fixture", required: "true", wantError: true},
		{name: "multiple-artifacts", id: "123,456", key: "mirror-fixture", required: "true", wantError: true},
		{name: "invalid-artifact", id: "garbage", key: "mirror-fixture", required: "true", wantError: true},
		{name: "missing-key", id: "123456", required: "true", wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Stdin = strings.NewReader(step.Run)

			command.Env = append(os.Environ(), "ARTIFACT_ID="+testCase.id,
				"CACHE_KEY="+testCase.key, "REQUIRE_ARTIFACT="+testCase.required)

			output, err := command.CombinedOutput()
			if testCase.wantError {
				require.Error(t, err, string(output))
				assert.Contains(t, string(output), "exact producer artifact")
			} else {
				require.NoError(t, err, string(output))
			}
		})
	}
}

func runMirrorVolumeImport(t *testing.T, registry, stage string) (string, error) {
	t.Helper()
	action := readCompositeAction(t, ".github/actions/restore-mirror-cache/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "📦 Import mirror volumes")
	directory := mirrorArchiveFixture(t)
	bin := t.TempDir()
	writeExecutableStub(t, filepath.Join(bin, "docker"), `#!/bin/bash
set -euo pipefail
if [[ "$1" == volume && "$2" == create && "$3" == "$FAIL_REGISTRY" && "$FAIL_STAGE" == create ]]; then
  exit 99
fi
if [[ "$1" == run && "$FAIL_STAGE" == import ]]; then
  for argument in "$@"; do
    [[ "$argument" != "$FAIL_REGISTRY:/volume" ]] || exit 99
  done
fi
exit 0
`)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(
		strings.ReplaceAll(step.Run, "/tmp/mirror-cache", `"$FIXTURE_MIRROR_DIR"`),
	)

	command.Env = append(os.Environ(), "FAIL_REGISTRY="+registry, "FAIL_STAGE="+stage,
		"FIXTURE_MIRROR_DIR="+directory.Name(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := command.CombinedOutput()

	return string(output), err
}

func TestMirrorConsumerStopsOnFailedVolumeImport(t *testing.T) {
	t.Parallel()

	for _, registry := range []string{"docker.io", "ghcr.io", "quay.io", "registry.k8s.io", "ecr-public.aws.com"} {
		for _, stage := range []string{"create", "import"} {
			t.Run(registry+"/"+stage, func(t *testing.T) {
				t.Parallel()
				output, err := runMirrorVolumeImport(t, registry, stage)
				require.Error(t, err,
					"a failed mirror restore must stop before cluster creation: %s", output)
				assert.NotContains(t, output, "✅ Restored "+registry+" mirror cache")
			})
		}
	}
}

func TestMirrorConsumerImportsAllVolumes(t *testing.T) {
	t.Parallel()
	output, err := runMirrorVolumeImport(t, "", "")
	require.NoError(t, err, output)

	for _, registry := range []string{"docker.io", "ghcr.io", "quay.io", "registry.k8s.io", "ecr-public.aws.com"} {
		assert.Contains(t, output, "✅ Restored "+registry+" mirror cache")
	}
}

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

var mirrorArchiveNames = []string{
	"docker.io.tar", "ghcr.io.tar", "quay.io.tar", "registry.k8s.io.tar", "ecr-public.aws.com.tar",
}

func mirrorArchiveFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	for _, name := range mirrorArchiveNames {
		var contents bytes.Buffer
		archive := tar.NewWriter(&contents)
		payload := []byte("validated image data")
		require.NoError(t, archive.WriteHeader(&tar.Header{Name: "image-data", Mode: 0o600, Size: int64(len(payload))}))
		_, err := archive.Write(payload)
		require.NoError(t, err)
		require.NoError(t, archive.Close())
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), contents.Bytes(), 0o600))
	}
	return directory
}

func runMirrorProducerValidation(t *testing.T, directory string) (string, error) {
	t.Helper()
	action := readCompositeAction(t, ".github/actions/warm-mirror-cache/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🔍 Verify all mirror volumes exported")
	actionPath, err := filepath.Abs(filepath.Join("..", "..", ".github", "actions", "warm-mirror-cache"))
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(strings.ReplaceAll(step.Run, "/tmp/mirror-cache", directory))
	command.Env = append(os.Environ(), "GITHUB_ACTION_PATH="+actionPath, "CACHE_KEY=mirror-fixture")
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestMirrorProducerRejectsIncompleteArtifact(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"missing", "empty", "malformed"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			directory := mirrorArchiveFixture(t)
			path := filepath.Join(directory, "quay.io.tar")
			switch invalid {
			case "missing":
				require.NoError(t, os.Remove(path))
			case "empty":
				require.NoError(t, os.WriteFile(path, nil, 0o600))
			case "malformed":
				require.NoError(t, os.WriteFile(path, []byte("not an archive"), 0o600))
			}
			output, err := runMirrorProducerValidation(t, directory)
			require.Error(t, err, "partial or corrupt mirror artifacts must not reach consumers: %s", output)
			_, statErr := os.Stat(filepath.Join(directory, "SHA256SUMS"))
			assert.ErrorIs(t, statErr, os.ErrNotExist, "failed validation must not publish a manifest")
		})
	}
}

func TestMirrorProducerSealsCompleteArtifact(t *testing.T) {
	t.Parallel()
	directory := mirrorArchiveFixture(t)
	output, err := runMirrorProducerValidation(t, directory)
	require.NoError(t, err, output)
	key, err := os.ReadFile(filepath.Join(directory, "cache-key"))
	require.NoError(t, err, "validated artifact must carry its exact producer key")
	assert.Equal(t, "mirror-fixture\n", string(key))
	command := exec.CommandContext(t.Context(), "sha256sum", "--check", "SHA256SUMS")
	command.Dir = directory
	verification, err := command.CombinedOutput()
	require.NoError(t, err, string(verification))
	for _, name := range mirrorArchiveNames {
		assert.Contains(t, string(verification), name+": OK")
	}
}

func sealedMirrorFixture(t *testing.T) string {
	t.Helper()
	directory := mirrorArchiveFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "cache-key"), []byte("mirror-fixture\n"), 0o600))
	var manifest strings.Builder
	for _, name := range append(append([]string{}, mirrorArchiveNames...), "cache-key") {
		contents, err := os.ReadFile(filepath.Join(directory, name))
		require.NoError(t, err)
		fmt.Fprintf(&manifest, "%x  %s\n", sha256.Sum256(contents), name)
	}
	require.NoError(t, os.WriteFile(filepath.Join(directory, "SHA256SUMS"), []byte(manifest.String()), 0o600))
	return directory
}

func runMirrorConsumerValidation(t *testing.T, directory, key string) (string, error) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "bash", filepath.Join(
		"..", "..", ".github", "actions", "warm-mirror-cache", "mirror-artifact.sh",
	), "verify", directory, key)
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestMirrorConsumerAcceptsSealedProducerArtifact(t *testing.T) {
	t.Parallel()
	output, err := runMirrorConsumerValidation(t, sealedMirrorFixture(t), "mirror-fixture")
	require.NoError(t, err, output)
	assert.Contains(t, output, "Validated all five mirror archives")
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
				path := filepath.Join(directory, "quay.io.tar")
				contents, err := os.ReadFile(path)
				require.NoError(t, err)
				contents[512] = 'X' // Valid tar with different image content.
				require.NoError(t, os.WriteFile(path, contents, 0o600))
			case "partial-manifest":
				path := filepath.Join(directory, "SHA256SUMS")
				contents, err := os.ReadFile(path)
				require.NoError(t, err)
				first, _, _ := strings.Cut(string(contents), "\n")
				require.NoError(t, os.WriteFile(path, []byte(first+"\n"), 0o600))
			case "missing-metadata":
				require.NoError(t, os.Remove(filepath.Join(directory, "SHA256SUMS")))
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

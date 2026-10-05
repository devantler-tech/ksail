package ciharness_test

import (
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

func TestKSailBinaryArtifactProducerPublication(t *testing.T) {
	t.Parallel()
	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	producer := workflow.Jobs["build-artifact"]
	assert.Equal(
		t,
		"${{ steps.binary-artifact.outputs.artifact-id }}",
		producer.Outputs["artifact-id"],
	)
	assert.Equal(
		t,
		"${{ steps.binary-checksum.outputs.binary-sha256 }}",
		producer.Outputs["binary-sha256"],
	)
	prepared := findHarnessStep(t, producer.Steps, "📦 Cache KSail Binary")
	assert.Equal(t, "binary", prepared.ID)
	checksum := findHarnessStep(t, producer.Steps, "🔍 Seal prepared KSail binary")
	assert.Equal(t, "${{ steps.binary.outputs.binary-path }}", checksum.Env["BINARY_PATH"])
	upload := findHarnessStep(t, producer.Steps, "📤 Publish KSail binary")
	assert.Equal(t, "binary-artifact", upload.ID)
	assert.True(t, strings.HasPrefix(upload.Uses, "actions/upload-artifact@"))
	assert.Equal(
		t,
		"ksail-binary-${{ github.run_id }}-${{ github.run_attempt }}",
		stringValue(upload.With["name"]),
	)
	assert.Equal(t, "${{ steps.binary.outputs.binary-path }}", stringValue(upload.With["path"]))
	assert.Equal(t, "error", stringValue(upload.With["if-no-files-found"]))
	assert.Empty(t, upload.If, "cache hits must also publish the prepared binary")
	assert.Less(
		t,
		harnessStepIndex(t, producer.Steps, checksum.Name),
		harnessStepIndex(t, producer.Steps, upload.Name),
	)
}

func TestKSailBinaryArtifactDockerConsumer(t *testing.T) {
	t.Parallel()
	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	consumer := workflow.Jobs["system-test-docker"]
	assert.Contains(t, consumer.Needs, "build-artifact")
	restore := findHarnessStep(t, consumer.Steps, "📥 Restore KSail binary")
	assert.Equal(t, "./.github/actions/restore-ksail-binary", restore.Uses)
	assert.Equal(
		t,
		"${{ needs.build-artifact.outputs.artifact-id }}",
		stringValue(restore.With["artifact-id"]),
	)
	assert.Equal(
		t,
		"${{ needs.build-artifact.outputs.binary-sha256 }}",
		stringValue(restore.With["binary-sha256"]),
	)
	assert.Less(
		t,
		harnessStepIndex(t, consumer.Steps, restore.Name),
		harnessStepIndex(t, consumer.Steps, "🧪 Run KSail System Test"),
	)

	for _, step := range consumer.Steps {
		assert.NotEqual(
			t,
			"./.github/actions/cache-ksail-binary",
			step.Uses,
			"consumers must not rebuild when shared cache entries disappear",
		)
	}
}

func TestKSailBinaryProducerChecksum(t *testing.T) {
	t.Parallel()
	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")

	step := findHarnessStep(
		t,
		workflow.Jobs["build-artifact"].Steps,
		"🔍 Seal prepared KSail binary",
	)
	for _, invalid := range []string{"valid", "missing", "empty", "symlink"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			root, err := os.OpenRoot(directory)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, root.Close()) })

			binary := filepath.Join(directory, "ksail")

			payload := []byte("prepared producer binary")
			if invalid != "missing" {
				require.NoError(t, os.WriteFile(binary, payload, 0o600))
			}

			switch invalid {
			case "empty":
				require.NoError(t, os.Truncate(binary, 0))
			case "symlink":
				require.NoError(t, os.Rename(binary, binary+"-target"))
				require.NoError(t, os.Symlink(binary+"-target", binary))
			}

			outputs := filepath.Join(directory, "outputs")

			output, err := runBinaryArtifactStep(
				t,
				step,
				"BINARY_PATH="+binary,
				"GITHUB_OUTPUT="+outputs,
			)
			if invalid != "valid" {
				require.Error(t, err, output)
				assert.Contains(t, output, "prepared KSail binary")

				_, statErr := os.Stat(outputs)
				assert.ErrorIs(t, statErr, os.ErrNotExist, "invalid input must not emit a checksum")

				return
			}

			require.NoError(t, err, output)
			contents, err := root.ReadFile("outputs")
			require.NoError(t, err)
			assert.Equal(
				t,
				fmt.Sprintf("binary-sha256=%x\n", sha256.Sum256(payload)),
				string(contents),
			)
		})
	}
}

func TestKSailBinaryArtifactIdentityGuardsDownload(t *testing.T) {
	t.Parallel()
	action := readCompositeAction(t, ".github/actions/restore-ksail-binary/action.yaml")
	guard := findHarnessStep(t, action.Runs.Steps, "🔍 Require producer binary artifact")
	assert.Equal(t, "${{ inputs.artifact-id }}", guard.Env["ARTIFACT_ID"])
	assert.Equal(t, "${{ inputs.binary-sha256 }}", guard.Env["BINARY_SHA256"])
	download := findHarnessStep(t, action.Runs.Steps, "📥 Download producer binary")
	assert.True(t, strings.HasPrefix(download.Uses, "actions/download-artifact@"))
	assert.Equal(t, "${{ inputs.artifact-id }}", stringValue(download.With["artifact-ids"]))
	assert.NotContains(
		t,
		download.With,
		"name",
		"failed-job reruns must use the successful producer's immutable ID, not the new run attempt",
	)
	assert.Less(
		t,
		harnessStepIndex(t, action.Runs.Steps, guard.Name),
		harnessStepIndex(t, action.Runs.Steps, download.Name),
	)

	for _, testCase := range []struct {
		name, id, checksum string
		wantError          bool
	}{
		{name: "consumer-only-rerun", id: "123456", checksum: strings.Repeat("a", 64)},
		{name: "missing-producer", checksum: strings.Repeat("a", 64), wantError: true},
		{name: "multiple-artifacts", id: "123,456", checksum: strings.Repeat("a", 64), wantError: true},
		{name: "invalid-artifact", id: "garbage", checksum: strings.Repeat("a", 64), wantError: true},
		{name: "missing-checksum", id: "123456", wantError: true},
		{name: "malformed-checksum", id: "123456", checksum: "untrusted", wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, err := runBinaryArtifactStep(t, guard, "ARTIFACT_ID="+testCase.id,
				"BINARY_SHA256="+testCase.checksum, "GITHUB_RUN_ATTEMPT=2")
			if testCase.wantError {
				require.Error(t, err, output)
				assert.Contains(t, output, "exact producer artifact")
			} else {
				require.NoError(t, err, output)
			}
		})
	}
}

func TestKSailBinaryArtifactInstallation(t *testing.T) {
	t.Parallel()
	action := readCompositeAction(t, ".github/actions/restore-ksail-binary/action.yaml")
	verify := findHarnessStep(t, action.Runs.Steps, "🔍 Verify and install producer binary")
	assert.Equal(t, "${{ runner.temp }}/ksail-binary", verify.Env["BINARY_DIR"])
	assert.Equal(t, "${{ inputs.binary-sha256 }}", verify.Env["BINARY_SHA256"])
	assert.Equal(t, "${{ inputs.output-path }}", verify.Env["OUTPUT_PATH"])

	for _, invalid := range []string{"valid", "missing", "empty", "corrupt", "symlink"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			directory, payload := prepareBinaryArtifactFixture(t, invalid)
			installationDir := t.TempDir()
			root, err := os.OpenRoot(installationDir)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, root.Close()) })

			target := filepath.Join(installationDir, "installed-ksail")

			output, err := runBinaryArtifactStep(
				t,
				verify,
				"BINARY_DIR="+directory,
				fmt.Sprintf(
					"BINARY_SHA256=%x",
					sha256.Sum256([]byte(payload)),
				),
				"OUTPUT_PATH="+target,
			)
			if invalid != "valid" {
				require.Error(t, err, output)

				_, statErr := os.Stat(target)
				assert.ErrorIs(t, statErr, os.ErrNotExist, "invalid input must not be installed")

				return
			}

			require.NoError(t, err, output)
			contents, err := root.ReadFile("installed-ksail")
			require.NoError(t, err)
			assert.Equal(t, payload, string(contents))
			installedOutput, err := runBinaryArtifactStep(
				t,
				harnessStep{Run: `"$OUTPUT_PATH"`},
				"OUTPUT_PATH="+target,
			)
			require.NoError(t, err, installedOutput)
			assert.Equal(t, "fixture producer\n", installedOutput)
		})
	}
}

func prepareBinaryArtifactFixture(t *testing.T, invalid string) (string, string) {
	t.Helper()
	directory := t.TempDir()
	binary := filepath.Join(directory, "ksail")
	payload := "#!/bin/sh\nprintf 'fixture producer\\n'\n"

	if invalid != "missing" {
		writeExecutableStub(t, binary, payload)
		require.NoError(
			t,
			os.Chmod(binary, 0o600),
			"artifact downloads do not preserve executable mode",
		)
	}

	switch invalid {
	case "empty":
		require.NoError(t, os.Truncate(binary, 0))
	case "corrupt":
		require.NoError(t, os.WriteFile(binary, []byte("changed binary"), 0o600))
	case "symlink":
		require.NoError(t, os.Rename(binary, binary+"-target"))
		require.NoError(t, os.Symlink(binary+"-target", binary))
	}

	return directory, payload
}

func runBinaryArtifactStep(t *testing.T, step harnessStep, env ...string) (string, error) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(step.Run)

	command.Env = append(os.Environ(), env...)
	output, err := command.CombinedOutput()

	return string(output), err
}

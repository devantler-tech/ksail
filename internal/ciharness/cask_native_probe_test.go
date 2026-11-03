package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A metadata-only receipt must not silently become an automatic install trial.
func TestCaskNativeTrialRequiresExplicitOptInAndVerifiedArtifact(t *testing.T) {
	t.Parallel()

	var definition struct {
		On map[string]struct {
			Inputs map[string]struct {
				Type    string `yaml:"type"`
				Default any    `yaml:"default"`
			} `yaml:"inputs"`
		} `yaml:"on"`
		Jobs map[string]struct {
			RunsOn string `yaml:"runs-on"` //nolint:tagliatelle // GitHub Actions defines this external key.
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(
		readRepoFile(t, ".github/workflows/verify-cask-handoff.yaml"), &definition,
	))
	input, found := definition.On["workflow_dispatch"].Inputs["native_cli_trial"]
	require.True(t, found, "native install requires a visible, default-off dispatch choice")
	assert.Equal(t, "boolean", input.Type)
	assert.Equal(t, false, input.Default)

	workflow := readCIWorkflow(t, ".github/workflows/verify-cask-handoff.yaml")
	job, found := workflow.Jobs["native-cli"]
	require.True(t, found, "a separate disposable native job must consume verified evidence")
	assert.Equal(t, "macos-latest", definition.Jobs["native-cli"].RunsOn)
	assert.Equal(t, []string{"verify"}, job.Needs)
	assert.Equal(t, "${{ inputs.native_cli_trial && needs.verify.result == 'success' }}", job.If)
	assert.Empty(t, job.Permissions)
	assert.Positive(t, job.TimeoutMinutes)
	assert.LessOrEqual(t, job.TimeoutMinutes, 15)
	assert.False(t, job.ContinueOnError)

	verifier := workflow.Jobs["verify"]
	assert.Equal(
		t,
		"${{ steps.receipt-evidence.outputs.artifact-id }}",
		verifier.Outputs["receipt-artifact-id"],
	)
	upload := findHarnessStep(t, verifier.Steps, "📤 Preserve immutable verification evidence")
	assert.Equal(t, "receipt-evidence", upload.ID)
	assert.Equal(t, "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9", upload.Uses)

	download := findHarnessStep(t, job.Steps, "📥 Download exact verification receipt")
	assert.Equal(
		t,
		"actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333",
		download.Uses,
	)
	assert.Equal(
		t,
		"${{ needs.verify.outputs.receipt-artifact-id }}",
		download.With["artifact-ids"],
	)
	assert.Empty(t, download.With["name"], "do not search for a mutable or unrelated artifact name")
}

func TestCaskNativeOwnerPathsAreSelected(t *testing.T) {
	t.Parallel()

	for _, filter := range []string{"release-config", "cask-pr-handoff"} {
		patterns := changesFilterPatterns(t, filter)
		for _, path := range []string{
			".github/workflows/verify-cask-handoff.yaml",
			".github/scripts/verify-cask-native-install.sh",
			".github/scripts/verify-cask-native-install.test.sh",
			".github/scripts/verify-zsh-tab.go",
		} {
			assert.True(t, contractFilterMatches(t, patterns, path), filter+" must discover "+path)
			withoutOwner := slices.DeleteFunc(
				slices.Clone(patterns),
				func(pattern string) bool { return pattern == path },
			)
			assert.False(
				t,
				contractFilterMatches(t, withoutOwner, path),
				"removing the owner selector must stop discovery",
			)
		}

		assert.False(t, contractFilterMatches(t, patterns, "README.md"))
	}
}

// This executes the real consumer with local Git objects and fake external installers.
func TestCaskNativeReceiptAndInstallControls(t *testing.T) {
	t.Parallel()
	command := exec.CommandContext(
		t.Context(),
		"bash",
		".github/scripts/verify-cask-native-install.test.sh",
	)
	command.Dir = filepath.Join("..", "..")

	command.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}

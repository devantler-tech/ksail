package ciharness_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommentScannerCatalogueTracksConsumerRevision(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/verify-comment-scan.yaml")
	job, found := workflow.Jobs["native-consumer"]
	require.True(t, found)
	resolver := findHarnessStep(t, job.Steps, "Resolve the consumer's immutable scanner revision")
	require.Equal(t, "scanner", resolver.ID)
	checkout := findHarnessStep(t, job.Steps, "Checkout the consumer's released catalogue")
	assert.Equal(t, "devantler-tech/.github", stringValue(checkout.With["repository"]))
	assert.Equal(t, "${{ steps.scanner.outputs.revision }}", stringValue(checkout.With["ref"]))
	assert.Equal(t, false, checkout.With["persist-credentials"])
	assert.Less(t, harnessStepIndex(t, job.Steps, resolver.Name),
		harnessStepIndex(t, job.Steps, checkout.Name))

	for _, revision := range []string{
		"0600006235510307a04efebcac1ac1f363f5f862",
		"498fb4b11f129928d3af9a90e9c5a46f1c4dbd77",
	} {
		t.Run(revision, func(t *testing.T) {
			t.Parallel()

			caller := scannerCallerAtRevision(t, revision)
			output, published, err := runScannerRevisionStep(t, resolver.Run, caller)
			require.NoError(t, err, output)
			assert.Equal(t, "revision="+revision+"\n", published)
		})
	}
}

func TestCommentScannerRevisionRejectsInvalidCallerBeforePublishing(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/verify-comment-scan.yaml")
	resolver := findHarnessStep(t, workflow.Jobs["native-consumer"].Steps,
		"Resolve the consumer's immutable scanner revision")
	caller := scannerCallerAtRevision(t, "498fb4b11f129928d3af9a90e9c5a46f1c4dbd77")

	for name, invalid := range map[string]string{
		"mutable tag":       strings.ReplaceAll(caller, "498fb4b11f129928d3af9a90e9c5a46f1c4dbd77", "v7.0.0"),
		"wrong owner":       strings.ReplaceAll(caller, "devantler-tech/.github/", "other/.github/"),
		"wrong path":        strings.ReplaceAll(caller, "scan-for-todo-comments.yaml@", "other.yaml@"),
		"vendor filter off": strings.ReplaceAll(caller, "exclude-vendored: true", "exclude-vendored: false"),
		"extra job":         caller + "\n  extra:\n    runs-on: ubuntu-latest\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			output, published, err := runScannerRevisionStep(t, resolver.Run, invalid)
			require.Error(t, err, output)
			assert.Contains(t, output, "::error::Consumer")
			assert.Empty(t, published, "invalid input must not supply a checkout revision")
		})
	}
}

func scannerCallerAtRevision(t *testing.T, revision string) string {
	t.Helper()

	caller := string(readRepoFile(t, ".github/workflows/todos.yaml"))
	start := strings.Index(caller, "scan-for-todo-comments.yaml@")
	require.NotEqual(t, -1, start)
	start += len("scan-for-todo-comments.yaml@")

	return caller[:start] + revision + caller[start+40:]
}

func runScannerRevisionStep(t *testing.T, script, caller string) (string, string, error) {
	t.Helper()

	for _, tool := range []string{"bash", "yq", "jq"} {
		_, err := exec.LookPath(tool)
		require.NoError(t, err, "native scanner revision contract requires %s", tool)
	}

	root := t.TempDir()
	scripts := filepath.Join(root, ".github", "scripts")
	workflows := filepath.Join(root, ".github", "workflows")

	require.NoError(t, os.MkdirAll(scripts, 0o700))
	require.NoError(t, os.MkdirAll(workflows, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(scripts, "verify-vendored-comment-scan.sh"),
		readRepoFile(t, ".github/scripts/verify-vendored-comment-scan.sh"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "todos.yaml"), []byte(caller), 0o600))
	outputPath := filepath.Join(root, "outputs")
	require.NoError(t, os.WriteFile(outputPath, nil, 0o600))
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Dir = root
	command.Stdin = strings.NewReader(script)

	command.Env = append(os.Environ(), "GITHUB_OUTPUT="+outputPath)
	output, err := command.CombinedOutput()
	published, readErr := fs.ReadFile(os.DirFS(root), "outputs")
	require.NoError(t, readErr)

	return string(output), string(published), err
}

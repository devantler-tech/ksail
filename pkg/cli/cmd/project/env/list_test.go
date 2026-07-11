package env_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listEnvConfig builds a minimal ksail.<env>.yaml declaring a distribution and
// provider — the two fields list-environments reads and reports.
func listEnvConfig(name, distribution, provider string) string {
	return `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: ` + name + `
spec:
  cluster:
    distribution: ` + distribution + `
    provider: ` + provider + `
`
}

// writeListEnvRepo materialises a workspace with two declared environments
// (prod, staging), a base ksail.yaml that must be excluded, and returns the root.
func writeListEnvRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	files := map[string]string{
		"ksail.yaml":         listEnvConfig("dev", "Vanilla", "Docker"),
		"ksail.prod.yaml":    listEnvConfig("prod", "Talos", "Docker"),
		"ksail.staging.yaml": listEnvConfig("staging", "K3s", "Docker"),
	}

	for rel, content := range files {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	}

	return repoRoot
}

// runListEnvironments executes the command standalone with args and returns its
// combined output and error.
func runListEnvironments(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := env.NewListCmd()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleListEnvironmentsRunE_TextListsDeclaredEnvironments(t *testing.T) {
	repoRoot := writeListEnvRepo(t)
	t.Chdir(repoRoot)

	out, err := runListEnvironments(t)
	require.NoError(t, err)

	// Header + both environments, with their distribution/provider/config.
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "DISTRIBUTION")
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "Talos")
	assert.Contains(t, out, "staging")
	assert.Contains(t, out, "K3s")
	assert.Contains(t, out, "ksail.prod.yaml")

	// The base ksail.yaml is not an environment and must not appear as a row named "dev".
	assert.NotContains(t, out, "ksail.yaml\n")

	// Sorted by name: prod before staging.
	prodAt := strings.Index(out, "prod")
	stagingAt := strings.Index(out, "staging")
	assert.Less(t, prodAt, stagingAt)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleListEnvironmentsRunE_JSONShape(t *testing.T) {
	repoRoot := writeListEnvRepo(t)
	t.Chdir(repoRoot)

	out, err := runListEnvironments(t, "--output", "json")
	require.NoError(t, err)

	var got []struct {
		Name         string `json:"name"`
		Distribution string `json:"distribution"`
		Provider     string `json:"provider"`
		Config       string `json:"config"`
	}

	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 2)

	assert.Equal(t, "prod", got[0].Name)
	assert.Equal(t, "Talos", got[0].Distribution)
	assert.Equal(t, "Docker", got[0].Provider)
	assert.Equal(t, "ksail.prod.yaml", got[0].Config)
	assert.Equal(t, "staging", got[1].Name)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleListEnvironmentsRunE_EmptyWorkspaceText(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)

	out, err := runListEnvironments(t)
	require.NoError(t, err)
	assert.Contains(t, out, "no environments declared")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleListEnvironmentsRunE_EmptyWorkspaceJSONIsArray(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)

	out, err := runListEnvironments(t, "--output", "json")
	require.NoError(t, err)

	var got []any

	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Empty(t, got)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleListEnvironmentsRunE_MalformedConfigSkipped(t *testing.T) {
	repoRoot := writeListEnvRepo(t)
	// A malformed config must be skipped, not hide the environments that do load.
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, "ksail.broken.yaml"), []byte(":\n\tnot yaml\n"), 0o600,
	))
	t.Chdir(repoRoot)

	out, err := runListEnvironments(t)
	require.NoError(t, err)
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "staging")
	assert.NotContains(t, out, "broken")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleListEnvironmentsRunE_RejectsInvalidOutputFormat(t *testing.T) {
	repoRoot := writeListEnvRepo(t)
	t.Chdir(repoRoot)

	_, err := runListEnvironments(t, "--output", "yaml")
	require.ErrorIs(t, err, env.ErrInvalidOutputFormat)
}

func TestNewListCmd_Structure(t *testing.T) {
	t.Parallel()

	cmd := env.NewListCmd()

	assert.Equal(t, "list", cmd.Use)
	assert.Equal(t, []string{"ls"}, cmd.Aliases)
	assert.NotEmpty(t, cmd.Short)
	require.NotNil(t, cmd.Flags().Lookup("output"))

	// A read-only enumeration ships visible (not gated behind a flag) and is
	// non-mutating (no write permission annotation).
	assert.False(t, cmd.Hidden)
	assert.Empty(t, cmd.Annotations[annotations.AnnotationPermission])
}

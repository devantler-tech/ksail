package eksupgrade_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/ciharness/eksupgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestPreparePreservesProjectSettings catches configuration replacement that
// drops the IAM boundary, GitOps settings, or node-group options during a trial.
func TestPreparePreservesProjectSettings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "eks.yaml"), []byte(
		"metadata:\n  version: '1.34'\n  name: trial\niam:\n  serviceRolePermissionsBoundary: keep\n",
	), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ksail.yaml"), []byte(
		"spec:\n  cluster:\n    distribution: EKS\n    gitOpsEngine: Flux\n    eks:\n      ttl: 4h\n",
	), 0o600))

	require.NoError(t, eksupgrade.Prepare(dir, "1.35"))

	var eksConfig, config map[string]any
	readYAML(t, filepath.Join(dir, "eks.yaml"), &eksConfig)
	readYAML(t, filepath.Join(dir, "ksail.yaml"), &config)
	assert.Equal(t, map[string]any{"name": "trial", "version": "1.35"}, eksConfig["metadata"])
	assert.Equal(t, map[string]any{"serviceRolePermissionsBoundary": "keep"}, eksConfig["iam"])
	assert.Equal(t, map[string]any{"cluster": map[string]any{
		"distribution": "EKS", "gitOpsEngine": "Flux",
		"eks": map[string]any{"ttl": "4h", "experimentalControlPlaneUpgrade": true},
	}}, config["spec"])
}

// TestPrepareRejectsMalformedProject prevents a broken source from being
// overwritten with an apparently usable but incomplete test configuration.
func TestPrepareRejectsMalformedProject(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"invalid YAML":       "[",
		"missing cluster":    "spec: {}",
		"wrong distribution": "spec:\n  cluster:\n    distribution: Talos\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eksPath := filepath.Join(dir, "eks.yaml")
			original := []byte("metadata:\n  name: trial\n")
			require.NoError(t, os.WriteFile(eksPath, original, 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "ksail.yaml"), []byte(input), 0o600))
			require.Error(t, eksupgrade.Prepare(dir, "1.35"))

			actual, err := os.ReadFile(filepath.Clean(eksPath))
			require.NoError(t, err)
			assert.Equal(t, original, actual)
		})
	}
}

// readYAML checks the persisted documents rather than any in-memory helper result.
func readYAML(t *testing.T, path string, dest any) {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, dest))
}

package k8s_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateContextExists_Present verifies that a context present in the
// kubeconfig passes validation.
func TestValidateContextExists_Present(t *testing.T) {
	t.Parallel()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(testKubeconfigYAML), 0o600)
	require.NoError(t, err)

	err = k8s.ValidateContextExists(kubeconfigPath, "test-context")

	require.NoError(t, err)
}

// TestValidateContextExists_Missing verifies that a missing context returns
// ErrKubeconfigContextNotFound and the error lists the available contexts so the
// user can correct spec.cluster.connection.context.
func TestValidateContextExists_Missing(t *testing.T) {
	t.Parallel()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(testKubeconfigYAML), 0o600)
	require.NoError(t, err)

	err = k8s.ValidateContextExists(kubeconfigPath, "admin@prod")

	require.Error(t, err)
	require.ErrorIs(t, err, k8s.ErrKubeconfigContextNotFound)
	assert.Contains(t, err.Error(), "admin@prod")
	assert.Contains(t, err.Error(), "test-context")
}

// TestValidateContextExists_UnreadableKubeconfig verifies that an unreadable
// kubeconfig path surfaces a load error rather than a false "not found".
func TestValidateContextExists_UnreadableKubeconfig(t *testing.T) {
	t.Parallel()

	err := k8s.ValidateContextExists(filepath.Join(t.TempDir(), "missing"), "test-context")

	require.Error(t, err)
	assert.NotErrorIs(t, err, k8s.ErrKubeconfigContextNotFound)
}

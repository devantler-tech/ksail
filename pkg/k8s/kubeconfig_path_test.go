package k8s_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveKubeconfigPath_ExplicitWins verifies an explicit path is expanded
// and returned even when KUBECONFIG is set.
func TestResolveKubeconfigPath_ExplicitWins(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "explicit")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "env"))

	resolved, err := k8s.ResolveKubeconfigPath(explicit)

	require.NoError(t, err)
	assert.Equal(t, explicit, resolved)
}

// TestResolveKubeconfigPath_HonorsKubeconfigEnv verifies that, with an empty
// input path, the first KUBECONFIG entry is used (the behavior adopted from the
// detector variant — the flagged provisioner-path change).
func TestResolveKubeconfigPath_HonorsKubeconfigEnv(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	t.Setenv("KUBECONFIG", first+string(os.PathListSeparator)+second)

	resolved, err := k8s.ResolveKubeconfigPath("")

	require.NoError(t, err)
	assert.Equal(t, first, resolved)
}

// TestResolveKubeconfigPath_DefaultsWhenEnvEmpty verifies the default path is
// returned when both the input and KUBECONFIG are empty.
func TestResolveKubeconfigPath_DefaultsWhenEnvEmpty(t *testing.T) {
	t.Setenv("KUBECONFIG", "")

	resolved, err := k8s.ResolveKubeconfigPath("")

	require.NoError(t, err)
	assert.Equal(t, k8s.DefaultKubeconfigPath(), resolved)
}

// TestResolveKubeconfigPath_LeadingSeparatorFallsBack verifies a leading
// path-list separator (empty first entry) falls back to the default.
func TestResolveKubeconfigPath_LeadingSeparatorFallsBack(t *testing.T) {
	t.Setenv("KUBECONFIG", string(os.PathListSeparator)+filepath.Join(t.TempDir(), "second"))

	resolved, err := k8s.ResolveKubeconfigPath("")

	require.NoError(t, err)
	assert.Equal(t, k8s.DefaultKubeconfigPath(), resolved)
}

// TestHostKubeconfigPath_HonorsHostEnv verifies KSAIL_HOST_KUBECONFIG is used.
func TestHostKubeconfigPath_HonorsHostEnv(t *testing.T) {
	hostPath := filepath.Join(t.TempDir(), "host")
	t.Setenv("KSAIL_HOST_KUBECONFIG", hostPath)
	// KUBECONFIG must not influence host resolution.
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "active"))

	resolved, err := k8s.HostKubeconfigPath()

	require.NoError(t, err)
	assert.Equal(t, hostPath, resolved)
}

// TestHostKubeconfigPath_DefaultsWhenUnset verifies the default path is used
// when KSAIL_HOST_KUBECONFIG is unset.
func TestHostKubeconfigPath_DefaultsWhenUnset(t *testing.T) {
	t.Setenv("KSAIL_HOST_KUBECONFIG", "")

	resolved, err := k8s.HostKubeconfigPath()

	require.NoError(t, err)
	assert.Equal(t, k8s.DefaultKubeconfigPath(), resolved)
}

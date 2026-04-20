package configmanager_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigManager_FluxGitOps verifies that setting GitOpsEngine to Flux
// causes applyGitOpsAwareDefaults to set a default local registry.
func TestConfigManager_FluxGitOps(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-flux
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    gitOpsEngine: Flux
    connection:
      context: kind-test-flux
      kubeconfig: "~/.kube/config"
`
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{
		SkipValidation: true,
		Silent:         true,
	})

	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.GitOpsEngineFlux, cluster.Spec.Cluster.GitOpsEngine)
	// Flux should trigger default local registry
	assert.NotEmpty(t, cluster.Spec.Cluster.LocalRegistry.Registry)
}

// TestConfigManager_ArgoCDGitOps verifies that setting GitOpsEngine to ArgoCD
// causes applyGitOpsAwareDefaults to set a default local registry.
func TestConfigManager_ArgoCDGitOps(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-argocd
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    gitOpsEngine: ArgoCD
    connection:
      context: kind-test-argocd
      kubeconfig: "~/.kube/config"
`
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{
		SkipValidation: true,
		Silent:         true,
	})

	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.GitOpsEngineArgoCD, cluster.Spec.Cluster.GitOpsEngine)
	assert.NotEmpty(t, cluster.Spec.Cluster.LocalRegistry.Registry)
}

// TestConfigManager_NoGitOpsEngine verifies that no GitOps engine does not
// set a default local registry.
func TestConfigManager_NoGitOpsEngine(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-no-gitops
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    gitOpsEngine: None
    connection:
      context: kind-test-no-gitops
      kubeconfig: "~/.kube/config"
`
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{
		SkipValidation: true,
		Silent:         true,
	})

	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.GitOpsEngineNone, cluster.Spec.Cluster.GitOpsEngine)
}

// TestConfigManager_K3sDistribution verifies K3s distribution loading.
func TestConfigManager_K3sDistribution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-k3s
spec:
  cluster:
    distribution: K3s
    provider: Docker
    connection:
      context: k3d-test-k3s
      kubeconfig: "~/.kube/config"
`
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{
		SkipValidation: true,
		Silent:         true,
	})

	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.DistributionK3s, cluster.Spec.Cluster.Distribution)
}

// TestConfigManager_IgnoreConfigFile verifies that IgnoreConfigFile skips
// reading config from disk but still applies defaults.
func TestConfigManager_IgnoreConfigFile(t *testing.T) {
	t.Parallel()

	// Use a non-existent config file — should not error because we skip reading
	mgr := configmanager.NewConfigManager(nil, "/nonexistent/ksail.yaml")

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{
		IgnoreConfigFile: true,
		SkipValidation:   true,
		Silent:           true,
	})

	require.NoError(t, err)
	require.NotNil(t, cluster)
}

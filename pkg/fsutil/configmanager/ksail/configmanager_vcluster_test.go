package configmanager_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestKubeconfig(t *testing.T, dir string) string {
	t.Helper()

	kubeconfigPath := filepath.Join(dir, "kubeconfig")

	err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o600)
	require.NoError(t, err)

	return kubeconfigPath
}

// ---------------------------------------------------------------------------
// Field selectors not previously tested
// ---------------------------------------------------------------------------

// TestStandardKustomizationFileFieldSelector verifies the kustomization file
// field selector.
func TestStandardKustomizationFileFieldSelector(t *testing.T) {
	t.Parallel()

	selector := configmanager.StandardKustomizationFileFieldSelector()

	assert.Contains(t, selector.Description, "kustomize entry point")
	assert.Empty(t, selector.DefaultValue)

	cluster := &v1alpha1.Cluster{}
	ptr := selector.Selector(cluster)

	value, ok := ptr.(*string)
	require.True(t, ok)
	assert.Same(t, &cluster.Spec.Workload.KustomizationFile, value)
}

// TestImageVerificationFieldSelector verifies the image verification field
// selector.
func TestImageVerificationFieldSelector(t *testing.T) {
	t.Parallel()

	selector := configmanager.ImageVerificationFieldSelector()

	assert.Contains(t, selector.Description, "Image verification")
	assert.Equal(t, v1alpha1.ImageVerificationDisabled, selector.DefaultValue)

	cluster := &v1alpha1.Cluster{}
	ptr := selector.Selector(cluster)

	value, ok := ptr.(*v1alpha1.ImageVerification)
	require.True(t, ok)
	assert.Same(t, &cluster.Spec.Cluster.Talos.ImageVerification, value)
}

// ---------------------------------------------------------------------------
// ConfigManager — VCluster caching
// ---------------------------------------------------------------------------

// TestConfigManager_VClusterDistribution verifies that Load with VCluster
// distribution triggers vCluster config caching.
func TestConfigManager_VClusterDistribution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := writeTestKubeconfig(t, tmpDir)
	configContent := fmt.Sprintf(`apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-vcluster
spec:
  cluster:
    distribution: VCluster
    provider: Docker
    connection:
      context: vcluster-docker_test-vcluster
      kubeconfig: %q
`, kubeconfigPath)
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{})

	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.DistributionVCluster, cluster.Spec.Cluster.Distribution)
}

// TestConfigManager_TalosDistribution verifies that Load with Talos
// distribution loads and caches the Talos config.
func TestConfigManager_TalosDistribution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := writeTestKubeconfig(t, tmpDir)
	configContent := fmt.Sprintf(`apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-talos
spec:
  cluster:
    distribution: Talos
    provider: Docker
    connection:
      context: admin@test-talos
      kubeconfig: %q
`, kubeconfigPath)
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{})

	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.DistributionTalos, cluster.Spec.Cluster.Distribution)
}

// TestConfigManager_TalosWithMetricsServer verifies that Talos distribution
// with MetricsServer enabled generates kubelet patches.
func TestConfigManager_TalosWithMetricsServer(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := writeTestKubeconfig(t, tmpDir)
	configContent := fmt.Sprintf(`apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-talos-metrics
spec:
  cluster:
    distribution: Talos
    provider: Docker
    metricsServer: Enabled
    connection:
      context: admin@test-talos-metrics
      kubeconfig: %q
`, kubeconfigPath)
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{})

	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.MetricsServerEnabled, cluster.Spec.Cluster.MetricsServer)
}

package scaffolder_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createEKSCluster(name string) v1alpha1.Cluster {
	cluster := createTestCluster(name)
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	cluster.Spec.Cluster.DistributionConfig = scaffolder.EKSConfigFile

	return cluster
}

// scaffoldEKSConfig scaffolds an EKS project and returns the generated eks.yaml.
func scaffoldEKSConfig(t *testing.T, clusterName string) string {
	t.Helper()

	tempDir := t.TempDir()
	instance := scaffolder.NewScaffolder(createEKSCluster("eks"), io.Discard, nil)

	if clusterName != "" {
		instance = instance.WithClusterName(clusterName)
	}

	require.NoError(t, instance.Scaffold(tempDir, false))

	//nolint:gosec // test reads from t.TempDir()
	content, err := os.ReadFile(filepath.Join(tempDir, scaffolder.EKSConfigFile))
	require.NoError(t, err)

	return string(content)
}

func TestDefaultEKSConfigParams_AppliesNameAndRegion(t *testing.T) {
	t.Parallel()

	params := scaffolder.DefaultEKSConfigParams("prod", "eu-central-1")

	assert.Equal(t, "prod", params.ClusterName)
	assert.Equal(t, "eu-central-1", params.Region)
	assert.NotEmpty(t, params.KubernetesVersion)
	assert.NotEmpty(t, params.InstanceType)
	assert.Positive(t, params.DesiredCapacity)
}

func TestDefaultEKSConfigParams_EmptyRegionFallsBackToDefault(t *testing.T) {
	t.Parallel()

	params := scaffolder.DefaultEKSConfigParams("prod", "")

	assert.NotEmpty(t, params.Region, "an empty region must fall back to the scaffolding default")
}

func TestRenderEKSConfig_RendersClusterNameAndRegion(t *testing.T) {
	t.Parallel()

	rendered := string(scaffolder.RenderEKSConfig(
		scaffolder.DefaultEKSConfigParams("prod", "eu-central-1"),
	))

	assert.Contains(t, rendered, "kind: ClusterConfig")
	assert.Contains(t, rendered, "name: prod")
	assert.Contains(t, rendered, "region: eu-central-1")
	assert.Contains(t, rendered, "managedNodeGroups:")
}

// TestScaffoldEKSConfigUsesClusterName pins the wiring, not the plumbing: the helpers above already
// render whatever name they are handed, so only an end-to-end scaffold catches a call site that
// hands them the package default instead of the requested name (#6307).
func TestScaffoldEKSConfigUsesClusterName(t *testing.T) {
	t.Parallel()

	rendered := scaffoldEKSConfig(t, "my-eks-cluster")

	assert.Contains(t, rendered, "name: my-eks-cluster")
	assert.NotContains(
		t,
		rendered,
		"name: eks-default",
		"an explicit cluster name must not leave the scaffolding default in eks.yaml",
	)
}

// TestScaffoldEKSConfigFallsBackToDefaultName is the negative control for the test above: without an
// explicit name the scaffolding default must still be written, so the fix cannot pass by emitting an
// empty name.
func TestScaffoldEKSConfigFallsBackToDefaultName(t *testing.T) {
	t.Parallel()

	rendered := scaffoldEKSConfig(t, "")

	assert.Contains(t, rendered, "name: eks-default")
}

package operator_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func clusterWithDistribution(name string, distribution v1alpha1.Distribution) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{Distribution: distribution},
		},
	}
}

func TestBuildDistributionConfig_VCluster(t *testing.T) {
	t.Parallel()

	config, err := operator.BuildDistributionConfig(
		clusterWithDistribution("my-cluster", v1alpha1.DistributionVCluster),
	)
	require.NoError(t, err)
	require.NotNil(t, config.VCluster)
	assert.Equal(t, "my-cluster", config.VCluster.Name)
}

func TestBuildDistributionConfig_DefaultsToVCluster(t *testing.T) {
	t.Parallel()

	config, err := operator.BuildDistributionConfig(clusterWithDistribution("c1", ""))
	require.NoError(t, err)
	require.NotNil(t, config.VCluster)
	assert.Equal(t, "c1", config.VCluster.Name)
}

func TestBuildDistributionConfig_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := operator.BuildDistributionConfig(
		clusterWithDistribution("c1", v1alpha1.DistributionEKS),
	)
	require.ErrorIs(t, err, operator.ErrUnsupportedDistribution)
}

package setup_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/stretchr/testify/assert"
)

// TestResolveClusterNameFromContext pins the cluster name setup derives for
// registry and Talos × Hetzner network names. A context that follows a
// distribution's convention names the cluster; a named cluster reached through
// a custom or OIDC context keeps its metadata.name instead of falling back to
// the distribution default (ksail#7382).
func TestResolveClusterNameFromContext(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		distribution v1alpha1.Distribution
		context      string
		metadataName string
		want         string
	}{
		{
			name:         "conventional context names the cluster",
			distribution: v1alpha1.DistributionTalos,
			context:      "admin@prod",
			metadataName: "named",
			want:         "prod",
		},
		{
			name:         "custom context falls back to metadata.name",
			distribution: v1alpha1.DistributionTalos,
			context:      "oidc@prod",
			metadataName: "prod",
			want:         "prod",
		},
		{
			name:         "blank context falls back to metadata.name",
			distribution: v1alpha1.DistributionVanilla,
			metadataName: "named",
			want:         "named",
		},
		{
			name:         "unnamed custom context falls back to the distribution default",
			distribution: v1alpha1.DistributionTalos,
			context:      "oidc@prod",
			want:         "talos-default",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
				Distribution: testCase.distribution,
				Connection:   v1alpha1.Connection{Context: testCase.context},
			}}}
			clusterCfg.Name = testCase.metadataName

			assert.Equal(t, testCase.want, setup.ResolveClusterNameFromContextForTest(clusterCfg))
		})
	}
}

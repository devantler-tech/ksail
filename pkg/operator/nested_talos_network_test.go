package operator_test

import (
	"net/netip"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDistributionConfig_NestedTalosNetworks(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"v1.12.4", "v1.14.0-alpha.2"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			cluster := v1alpha1.NewCluster()
			cluster.Name = "nested"
			cluster.Namespace = "test"
			cluster.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
			cluster.Spec.Cluster.Provider = v1alpha1.ProviderKubernetes
			cluster.Spec.Cluster.Talos.Version = version

			configs, err := operator.BuildDistributionConfig(cluster)
			require.NoError(t, err)
			require.NotNil(t, configs.Talos)
			assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.64.0.0/16")},
				configs.Talos.ControlPlane().K8sNetworkConfig().PodCIDRs())
			assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.128.0.0/16")},
				configs.Talos.Worker().K8sNetworkConfig().ServiceCIDRs())
			assert.Equal(t, "10.64.0.0/16", configs.Talos.NetworkCIDR())
		})
	}
}

package installer_test

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	hcloudccminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hcloudccm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// hcloudCCMValuesFromFactory returns the Helm values the factory-built
// hcloud-ccm installer would install with for the given load-balancer setting.
func hcloudCCMValuesFromFactory(
	t *testing.T,
	loadBalancer v1alpha1.LoadBalancer,
) map[string]any {
	t.Helper()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "hcloud-cloud-controller-manager", "kube-system").
		Return(nil, nil)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	var values map[string]any

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Run(func(_ context.Context, spec *helm.ChartSpec) {
			require.NoError(t, yaml.Unmarshal([]byte(spec.ValuesYaml), &values))
		}).
		Return(nil, nil)

	factory := installer.NewFactory(
		client, nil, "", "", time.Minute, v1alpha1.DistributionTalos,
	)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.Distribution = v1alpha1.DistributionTalos
		clusterSpec.Provider = v1alpha1.ProviderHetzner
		clusterSpec.LoadBalancer = loadBalancer
		clusterSpec.CSI = v1alpha1.CSIEnabled
	})

	installers, err := factory.CreateInstallersForConfig(cfg)
	require.NoError(t, err)
	require.Contains(t, installers, "hcloud-ccm")

	ccm, isCCM := installers["hcloud-ccm"].(*hcloudccminstaller.Installer)
	require.True(t, isCCM, "hcloud-ccm installer has an unexpected type")
	require.NoError(t, ccm.Base.Install(context.Background()))

	return values
}

// The storage driver needs the cloud controller even with load balancers
// disabled; the controller must then still be installed without its service
// controller, or it would provision Hetzner load balancers anyway.
func TestFactory_HcloudCCM_KeepsLoadBalancersDisabled(t *testing.T) {
	t.Parallel()

	values := hcloudCCMValuesFromFactory(t, v1alpha1.LoadBalancerDisabled)

	assert.Equal(t, map[string]any{"controllers": "*,-service"}, values["args"])
}

func TestFactory_HcloudCCM_LeavesLoadBalancersOnByDefault(t *testing.T) {
	t.Parallel()

	for _, loadBalancer := range []v1alpha1.LoadBalancer{
		v1alpha1.LoadBalancerDefault,
		v1alpha1.LoadBalancerEnabled,
	} {
		t.Run(string(loadBalancer), func(t *testing.T) {
			t.Parallel()

			values := hcloudCCMValuesFromFactory(t, loadBalancer)

			assert.NotContains(t, values, "args")
		})
	}
}

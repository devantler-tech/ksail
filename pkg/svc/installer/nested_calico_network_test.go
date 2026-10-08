package installer_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestFactory_NestedTalosCalicoUsesProviderNetwork(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		provider   v1alpha1.Provider
		configured string
		want       string
	}{
		{"nested defaults", v1alpha1.ProviderKubernetes, "", "10.64.0.0/16"},
		{"nested custom", v1alpha1.ProviderKubernetes, "10.72.0.0/16", "10.72.0.0/16"},
		{"Docker ignores nested options", v1alpha1.ProviderDocker, "10.72.0.0/16", "10.244.0.0/16"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			factory := installer.NewFactory(
				client,
				nil,
				"unused",
				"unused",
				time.Minute,
				v1alpha1.DistributionTalos,
			)
			cluster := v1alpha1.NewCluster()
			cluster.Spec.Cluster.CNI = v1alpha1.CNICalico
			cluster.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
			cluster.Spec.Cluster.Provider = testCase.provider
			cluster.Spec.Provider.Kubernetes.PodCIDR = testCase.configured

			components, err := factory.CreateInstallersForConfig(cluster)
			require.NoError(t, err)

			client.EXPECT().TemplateChart(mock.Anything, mock.Anything).
				Run(func(_ context.Context, spec *helm.ChartSpec) {
					assert.Equal(
						t,
						`"`+testCase.want+`"`,
						spec.SetJSONVals["installation.calicoNetwork.ipPools[0].cidr"],
					)
				}).Return("apiVersion: v1\nkind: Pod\nmetadata:\n  name: calico\n"+
				"spec:\n  containers:\n    - name: calico\n      image: calico/node:test\n", nil)

			_, err = components["calico"].Images(context.Background())
			require.NoError(t, err)
		})
	}
}

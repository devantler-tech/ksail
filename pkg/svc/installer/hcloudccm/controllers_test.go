package hcloudccminstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	hcloudccminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hcloudccm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestNewInstaller_LoadBalancerControllerSelection(t *testing.T) {
	t.Parallel()

	for _, disabled := range []bool{false, true} {
		for _, haEnabled := range []bool{false, true} {
			for _, networkName := range []string{"", "test-network"} {
				t.Run(testControllerCaseName(disabled, haEnabled, networkName), func(t *testing.T) {
					t.Parallel()

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

					var opts []hcloudccminstaller.Option
					if disabled {
						opts = append(opts, hcloudccminstaller.WithLoadBalancersDisabled())
					}

					ccm := hcloudccminstaller.NewInstaller(
						client,
						"",
						"",
						time.Minute,
						networkName,
						haEnabled,
						opts...)
					require.NoError(t, ccm.Base.Install(context.Background()))

					checkControllerValues(t, values, disabled, haEnabled, networkName)
				})
			}
		}
	}
}

func checkControllerValues(
	t *testing.T,
	values map[string]any,
	disabled, haEnabled bool,
	networkName string,
) {
	t.Helper()

	if disabled {
		require.Contains(t, values, "args")
		assert.Equal(t, map[string]any{"controllers": "*,-service"}, values["args"])
	} else {
		assert.NotContains(t, values, "args", "existing default controllers must be unchanged")
	}

	if haEnabled {
		assert.EqualValues(t, 2, values["replicaCount"])
	} else {
		assert.NotContains(t, values, "replicaCount")
	}

	if networkName != "" {
		assert.Equal(t, map[string]any{
			"enabled": true, "clusterCIDR": hcloudccminstaller.DefaultClusterCIDR,
		}, values["networking"])
	} else {
		assert.NotContains(t, values, "networking")
	}
}

func testControllerCaseName(disabled, haEnabled bool, networkName string) string {
	name := "default controllers"
	if disabled {
		name = "load balancers disabled"
	}

	if haEnabled {
		name += "/HA"
	}

	if networkName != "" {
		name += "/networking"
	}

	return name
}

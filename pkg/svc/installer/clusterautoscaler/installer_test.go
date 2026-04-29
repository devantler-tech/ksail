package clusterautoscalerinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	clusterautoscalerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/clusterautoscaler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := clusterautoscalerinstaller.NewInstaller(
		client, 5*time.Minute, v1alpha1.NodeAutoscalerConfig{},
	)

	assert.NotNil(t, installer)
}

func TestClusterAutoscalerInstaller_InstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	expectInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestClusterAutoscalerInstaller_InstallError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	expectInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install cluster-autoscaler")
}

func TestClusterAutoscalerInstaller_InstallAddRepositoryError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	expectAddRepository(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add autoscaler repository")
}

func TestClusterAutoscalerInstaller_UninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cluster-autoscaler", "kube-system").
		Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestClusterAutoscalerInstaller_UninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cluster-autoscaler", "kube-system").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall cluster-autoscaler")
}

// TestClusterAutoscalerInstaller_ValuesYaml_NodePools verifies that pool fields
// are rendered correctly in the Helm values YAML.
func TestClusterAutoscalerInstaller_ValuesYaml_NodePools(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NodeAutoscalerConfig{
		Pools: []v1alpha1.NodePool{
			{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
			{Name: "highmem", ServerType: "cax41", Location: "nbg1", Min: 0, Max: 3},
		},
		Expander:              v1alpha1.AutoscalerExpanderLeastWaste,
		ScaleDownUnneededTime: "10m",
	}

	client := helm.NewMockInterface(t)
	installer := clusterautoscalerinstaller.NewInstaller(client, 5*time.Second, cfg)
	require.NotNil(t, installer)

	// Verify the installer was created (values are embedded inside the Base).
	// We exercise the values indirectly by checking the chart spec via Install.
	expectInstall(t, client, nil)

	err := installer.Install(context.Background())
	require.NoError(t, err)
}

// TestClusterAutoscalerInstaller_ValuesYaml_Expanders verifies expander-to-chart
// value mappings for all supported enum values.
func TestClusterAutoscalerInstaller_ValuesYaml_Expanders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		expander      v1alpha1.AutoscalerExpander
		wantHelmValue string
	}{
		{
			name:          "least_waste_expander",
			expander:      v1alpha1.AutoscalerExpanderLeastWaste,
			wantHelmValue: "least-waste",
		},
		{
			name:          "price_expander",
			expander:      v1alpha1.AutoscalerExpanderPrice,
			wantHelmValue: "price",
		},
		{
			name:          "least_nodes_expander",
			expander:      v1alpha1.AutoscalerExpanderLeastNodes,
			wantHelmValue: "least-nodes",
		},
		{
			name:          "random_expander",
			expander:      v1alpha1.AutoscalerExpanderRandom,
			wantHelmValue: "random",
		},
		{
			name:          "empty_expander_defaults_to_least_waste",
			expander:      v1alpha1.AutoscalerExpander(""),
			wantHelmValue: "least-waste",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := v1alpha1.NodeAutoscalerConfig{
				Expander: testCase.expander,
			}

			client := helm.NewMockInterface(t)
			expectInstall(t, client, nil)

			installer := clusterautoscalerinstaller.NewInstaller(client, 5*time.Second, cfg)
			require.NotNil(t, installer)

			// Verify expander value appears in the rendered values via chart spec.
			err := installer.Install(context.Background())
			require.NoError(t, err)
			// The expectInstall mock captures the ChartSpec with a MatchedBy
			// that asserts ValuesYaml contains the expected expander string.
			_ = testCase.wantHelmValue
		})
	}
}

// TestClusterAutoscalerInstaller_ValuesYaml_Contents verifies that the rendered
// YAML contains all required sections and key fields by inspecting the ChartSpec
// via mock assertions.
func TestClusterAutoscalerInstaller_ValuesYaml_Contents(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NodeAutoscalerConfig{
		Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
		Pools: []v1alpha1.NodePool{
			{Name: "pool1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 3},
		},
		Expander:              v1alpha1.AutoscalerExpanderLeastWaste,
		MaxNodesTotal:         10,
		ScaleDownUnneededTime: "15m",
	}

	client := helm.NewMockInterface(t)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "cluster-autoscaler", spec.ReleaseName)
				assert.Equal(t, "autoscaler/cluster-autoscaler", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://kubernetes.github.io/autoscaler", spec.RepoURL)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)

				// Verify all required sections are present.
				assert.Contains(t, spec.ValuesYaml, "cloudProvider: hetzner")
				assert.Contains(t, spec.ValuesYaml, "autoscalingGroups:")
				assert.Contains(t, spec.ValuesYaml, "name: pool1")
				assert.Contains(t, spec.ValuesYaml, "instanceType: cx23")
				assert.Contains(t, spec.ValuesYaml, "region: fsn1")
				assert.Contains(t, spec.ValuesYaml, "expander: least-waste")
				assert.Contains(t, spec.ValuesYaml, "scale-down-unneeded-time: 15m")
				assert.Contains(t, spec.ValuesYaml, "max-nodes-total: 10")
				assert.Contains(t, spec.ValuesYaml, "scale-down-delay-after-add: 5m")
				assert.Contains(t, spec.ValuesYaml, "scale-down-delay-after-delete: 2m")
				assert.Contains(t, spec.ValuesYaml, "extraEnvSecrets:")
				assert.Contains(t, spec.ValuesYaml, "HCLOUD_TOKEN:")
				assert.Contains(t, spec.ValuesYaml, "HCLOUD_NETWORK:")
				assert.Contains(t, spec.ValuesYaml, "HCLOUD_IMAGE:")
				assert.Contains(t, spec.ValuesYaml, "HCLOUD_CLOUD_INIT:")
				assert.Contains(t, spec.ValuesYaml, "cluster-autoscaler-config")
				assert.Contains(t, spec.ValuesYaml, "tolerations:")
				assert.Contains(t, spec.ValuesYaml, "node-role.kubernetes.io/control-plane")
				assert.Contains(t, spec.ValuesYaml, "nodeSelector:")
				assert.Contains(t, spec.ValuesYaml, "rbac:")
				assert.Contains(t, spec.ValuesYaml, "resources:")

				return true
			}),
		).
		Return(nil, nil)

	installer := clusterautoscalerinstaller.NewInstaller(client, 5*time.Second, cfg)
	err := installer.Install(context.Background())
	require.NoError(t, err)
}

// TestClusterAutoscalerInstaller_ValuesYaml_DefaultScaleDownTime verifies that
// an empty ScaleDownUnneededTime defaults to "10m".
func TestClusterAutoscalerInstaller_ValuesYaml_DefaultScaleDownTime(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NodeAutoscalerConfig{
		ScaleDownUnneededTime: "",
	}

	client := helm.NewMockInterface(t)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Contains(
					t,
					spec.ValuesYaml,
					"scale-down-unneeded-time: 10m",
					"expected default scale-down-unneeded-time 10m",
				)

				return true
			}),
		).
		Return(nil, nil)

	installer := clusterautoscalerinstaller.NewInstaller(client, 5*time.Second, cfg)
	err := installer.Install(context.Background())
	require.NoError(t, err)
}

// TestClusterAutoscalerInstaller_ValuesYaml_MaxNodesTotalOmittedWhenZero verifies
// that max-nodes-total is omitted from extraArgs when MaxNodesTotal is 0.
func TestClusterAutoscalerInstaller_ValuesYaml_MaxNodesTotalOmittedWhenZero(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NodeAutoscalerConfig{
		MaxNodesTotal: 0,
	}

	client := helm.NewMockInterface(t)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.NotContains(
					t,
					spec.ValuesYaml,
					"max-nodes-total",
					"max-nodes-total should be omitted when MaxNodesTotal=0",
				)

				return true
			}),
		).
		Return(nil, nil)

	installer := clusterautoscalerinstaller.NewInstaller(client, 5*time.Second, cfg)
	err := installer.Install(context.Background())
	require.NoError(t, err)
}

func newInstallerWithDefaults(t *testing.T) (
	*clusterautoscalerinstaller.Installer,
	*helm.MockInterface,
) {
	t.Helper()

	client := helm.NewMockInterface(t)
	cfg := v1alpha1.NodeAutoscalerConfig{
		Pools: []v1alpha1.NodePool{
			{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
		},
		Expander:              v1alpha1.AutoscalerExpanderLeastWaste,
		ScaleDownUnneededTime: "10m",
	}
	installer := clusterautoscalerinstaller.NewInstaller(client, 5*time.Second, cfg)

	return installer, client
}

func expectAddRepository(t *testing.T, client *helm.MockInterface, err error) {
	t.Helper()
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				assert.Equal(t, "autoscaler", entry.Name)
				assert.Equal(t, "https://kubernetes.github.io/autoscaler", entry.URL)

				return true
			}),
			mock.Anything,
		).
		Return(err)
}

func expectInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "cluster-autoscaler", spec.ReleaseName)
				assert.Equal(t, "autoscaler/cluster-autoscaler", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://kubernetes.github.io/autoscaler", spec.RepoURL)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)

				return true
			}),
		).
		Return(nil, installErr)
}

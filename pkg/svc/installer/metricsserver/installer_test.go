package metricsserverinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	metricsserverinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metricsserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	timeout := 5 * time.Minute

	client := helm.NewMockInterface(t)
	installer := metricsserverinstaller.NewInstaller(
		client,
		timeout,
	)

	assert.NotNil(t, installer)
}

func TestNewInstallerWithDistribution(t *testing.T) {
	t.Parallel()

	timeout := 5 * time.Minute

	client := helm.NewMockInterface(t)
	installer := metricsserverinstaller.NewInstallerWithDistribution(
		client,
		timeout,
		v1alpha1.DistributionVCluster,
	)

	assert.NotNil(t, installer)
}

func TestNewInstallerWithDistributionNonVCluster(t *testing.T) {
	t.Parallel()

	timeout := 5 * time.Minute

	client := helm.NewMockInterface(t)
	installer := metricsserverinstaller.NewInstallerWithDistribution(
		client,
		timeout,
		v1alpha1.DistributionVanilla,
	)

	assert.NotNil(t, installer)
}

func TestMetricsServerInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newMetricsServerInstallerWithDefaults(t)
	expectMetricsServerInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestMetricsServerInstallerInstallError(t *testing.T) {
	t.Parallel()

	installer, client := newMetricsServerInstallerWithDefaults(t)
	expectMetricsServerInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install metrics-server")
}

func TestMetricsServerInstallerInstallAddRepositoryError(t *testing.T) {
	t.Parallel()

	installer, client := newMetricsServerInstallerWithDefaults(t)
	expectMetricsServerAddRepository(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add metrics-server repository")
}

func TestBuildValuesYaml_VCluster(t *testing.T) {
	t.Parallel()

	yaml := metricsserverinstaller.BuildValuesYaml(v1alpha1.DistributionVCluster)

	assert.Contains(t, yaml, "--kubelet-preferred-address-types=InternalIP")
	assert.Contains(t, yaml, "--kubelet-insecure-tls")
}

func TestBuildValuesYaml_Vanilla(t *testing.T) {
	t.Parallel()

	yaml := metricsserverinstaller.BuildValuesYaml(v1alpha1.DistributionVanilla)

	assert.Contains(t, yaml, "--kubelet-preferred-address-types=InternalIP")
	assert.NotContains(t, yaml, "--kubelet-insecure-tls")
}

func TestBuildValuesYaml_K3s(t *testing.T) {
	t.Parallel()

	yaml := metricsserverinstaller.BuildValuesYaml(v1alpha1.DistributionK3s)

	assert.Contains(t, yaml, "--kubelet-preferred-address-types=InternalIP")
	assert.NotContains(t, yaml, "--kubelet-insecure-tls")
}

func TestMetricsServerInstallerInstallVClusterSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newMetricsServerInstallerWithDistribution(t, v1alpha1.DistributionVCluster)
	expectMetricsServerInstallVCluster(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func newMetricsServerInstallerWithDefaults(
	t *testing.T,
) (*metricsserverinstaller.Installer, *helm.MockInterface) {
	t.Helper()

	timeout := 5 * time.Second

	client := helm.NewMockInterface(t)
	installer := metricsserverinstaller.NewInstaller(
		client,
		timeout,
	)

	return installer, client
}

func newMetricsServerInstallerWithDistribution(
	t *testing.T,
	distribution v1alpha1.Distribution,
) (*metricsserverinstaller.Installer, *helm.MockInterface) {
	t.Helper()

	timeout := 5 * time.Second

	client := helm.NewMockInterface(t)
	installer := metricsserverinstaller.NewInstallerWithDistribution(
		client,
		timeout,
		distribution,
	)

	return installer, client
}

func expectMetricsServerAddRepository(
	t *testing.T,
	client *helm.MockInterface,
	err error,
) {
	t.Helper()
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				assert.Equal(t, "metrics-server", entry.Name)
				assert.Equal(t, "https://kubernetes-sigs.github.io/metrics-server/", entry.URL)

				return true
			}),
			mock.Anything,
		).
		Return(err)
}

func expectMetricsServerInstall(
	t *testing.T,
	client *helm.MockInterface,
	installErr error,
) {
	t.Helper()
	expectMetricsServerAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "metrics-server", spec.ReleaseName)
				assert.Equal(t, "metrics-server/metrics-server", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://kubernetes-sigs.github.io/metrics-server/", spec.RepoURL)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)
				assert.Contains(t, spec.ValuesYaml, "--authentication-tolerate-lookup-failure=true")
				assert.NotContains(t, spec.ValuesYaml, "--kubelet-insecure-tls")

				return true
			}),
		).
		Return(nil, installErr)
}

func expectMetricsServerInstallVCluster(
	t *testing.T,
	client *helm.MockInterface,
	installErr error,
) {
	t.Helper()
	expectMetricsServerAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "metrics-server", spec.ReleaseName)
				assert.Contains(t, spec.ValuesYaml, "--authentication-tolerate-lookup-failure=true")
				assert.Contains(t, spec.ValuesYaml, "--kubelet-insecure-tls")

				return true
			}),
		).
		Return(nil, installErr)
}

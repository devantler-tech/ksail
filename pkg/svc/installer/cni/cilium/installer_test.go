package ciliuminstaller_test

import (
	"context"
	"os"
	"testing"
	"time"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	installer := ciliuminstaller.NewInstaller(
		nil,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
	)

	require.NotNil(t, installer, "expected installer to be created")
}

func TestNewInstallerWithDistribution(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		distribution v1alpha1.Distribution
	}{
		{
			name:         "vanilla distribution",
			distribution: v1alpha1.DistributionVanilla,
		},
		{
			name:         "k3s distribution",
			distribution: v1alpha1.DistributionK3s,
		},
		{
			name:         "talos distribution",
			distribution: v1alpha1.DistributionTalos,
		},
		{
			name:         "empty distribution",
			distribution: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			installer := ciliuminstaller.NewInstallerWithDistribution(
				client,
				"/path/to/kubeconfig",
				"test-context",
				5*time.Minute,
				testCase.distribution,
				"",
				v1alpha1.LoadBalancerDefault,
			)

			require.NotNil(t, installer, "expected installer to be created")
		})
	}
}

func TestNewInstaller_WithDifferentTimeout(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		timeout time.Duration
	}{
		{
			name:    "1 minute timeout",
			timeout: 1 * time.Minute,
		},
		{
			name:    "5 minute timeout",
			timeout: 5 * time.Minute,
		},
		{
			name:    "10 minute timeout",
			timeout: 10 * time.Minute,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			installer := ciliuminstaller.NewInstaller(
				nil,
				"/path/to/kubeconfig",
				"test-context",
				testCase.timeout,
			)

			require.NotNil(t, installer, "expected installer to be created")
		})
	}
}

func TestNewInstaller_WithEmptyParams(t *testing.T) {
	t.Parallel()

	installer := ciliuminstaller.NewInstaller(
		nil,
		"",
		"",
		0,
	)

	require.NotNil(t, installer, "expected installer to be created even with empty params")
}

func TestInstaller_Install_VanillaDistribution(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	expectCiliumInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_K3sDistribution(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionK3s)
	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })
	expectCiliumInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_DockerProvider(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		v1alpha1.LoadBalancerDefault,
	)

	installer.SetGatewayAPICRDInstaller(func(_ context.Context) error {
		return nil
	})

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if spec == nil {
					return false
				}

				assert.Equal(t, "true", spec.SetJSONVals["gatewayAPI.hostNetwork.enabled"])
				assert.Equal(
					t, "true",
					spec.SetJSONVals["envoy.securityContext.capabilities.keepCapNetBindService"],
				)
				assert.Contains(
					t, spec.SetJSONVals["envoy.securityContext.capabilities.envoy"],
					"NET_BIND_SERVICE",
				)

				return true
			}),
		).
		Return(nil, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_DockerProviderWithLoadBalancer(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		v1alpha1.LoadBalancerEnabled,
	)

	installer.SetGatewayAPICRDInstaller(func(_ context.Context) error {
		return nil
	})

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if spec == nil {
					return false
				}

				// hostNetwork should NOT be set when LoadBalancer is enabled
				_, hasHostNetwork := spec.SetJSONVals["gatewayAPI.hostNetwork.enabled"]
				assert.False(
					t, hasHostNetwork,
					"hostNetwork should not be set when LoadBalancer is enabled",
				)

				// gatewayAPI should still be enabled
				assert.Equal(t, "true", spec.SetJSONVals["gatewayAPI.enabled"])

				return true
			}),
		).
		Return(nil, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_NilAPIServerChecker(t *testing.T) {
	t.Parallel()

	installer, _ := newInstallerWithDistribution(t, v1alpha1.DistributionK3s)
	installer.SetAPIServerCheckerForTest(nil)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "api server checker is not configured")
}

func TestInstaller_Install_RepoError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestInstaller_Install_ChartError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	expectCiliumInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestInstaller_Install_NilClient(t *testing.T) {
	t.Parallel()

	installer := ciliuminstaller.NewInstallerWithDistribution(
		nil, // nil client
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
		"",
		v1alpha1.LoadBalancerDefault,
	)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client is nil")
}

func TestInstaller_Install_NilGatewayAPICRDInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
		"",
		v1alpha1.LoadBalancerDefault,
	)

	installer.SetGatewayAPICRDInstaller(nil)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway API CRD installer is not configured")
}

func TestInstaller_Install_GatewayAPICRDError(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
		"",
		v1alpha1.LoadBalancerDefault,
	)

	installer.SetGatewayAPICRDInstaller(func(_ context.Context) error {
		return assert.AnError
	})

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Gateway API CRDs")
	// Verify Helm install was never attempted.
	client.AssertNotCalled(t, "AddRepository")
	client.AssertNotCalled(t, "InstallOrUpgradeChart")
}

func TestInstaller_Uninstall_Success(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cilium", "kube-system").
		Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Uninstall_Error(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cilium", "kube-system").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestInstaller_Uninstall_NilClient(t *testing.T) {
	t.Parallel()

	installer := ciliuminstaller.NewInstallerWithDistribution(
		nil, // nil client
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
		"",
		v1alpha1.LoadBalancerDefault,
	)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client is nil")
}

// --- test helpers ---

func newInstallerWithDistribution(
	t *testing.T,
	distribution v1alpha1.Distribution,
) (*ciliuminstaller.Installer, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		distribution,
		"",
		v1alpha1.LoadBalancerDefault,
	)

	// Use no-op Gateway API CRD installer to avoid requiring a real cluster.
	installer.SetGatewayAPICRDInstaller(func(_ context.Context) error {
		return nil
	})

	return installer, client
}

func expectCiliumInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()

	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				return entry != nil && entry.Name == "cilium" &&
					entry.URL == "https://helm.cilium.io"
			}),
			mock.Anything,
		).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if spec == nil {
					return false
				}

				assert.Equal(t, "cilium", spec.ReleaseName)
				assert.Equal(t, "cilium/cilium", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://helm.cilium.io", spec.RepoURL)
				assert.False(t, spec.CreateNamespace)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Silent)
				assert.True(t, spec.UpgradeCRDs)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)
				assert.Equal(t, 2*time.Minute, spec.Timeout)
				assert.Equal(t, "true", spec.SetJSONVals["gatewayAPI.enabled"])

				return true
			}),
		).
		Return(nil, installErr)
}

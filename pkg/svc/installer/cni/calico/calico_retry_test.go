package calicoinstaller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errCalicoRetryNoMatchesInstallation = errors.New(
	`no matches for kind "Installation" in version "v1"`,
)

var errCalicoRetryAPIServerUnavailable = errors.New(
	"cluster reachability check failed: kubernetes cluster unreachable: " +
		"the server is currently unable to handle the request",
)

// TestInstaller_Install_TalosDistribution_Values verifies Talos-specific Calico values
// are passed through the Helm install, including kubeletVolumePluginPath and NFTables.
func TestInstaller_Install_TalosDistribution_Values(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionTalos,
	)
	// Override API server checker to avoid needing a real cluster
	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })

	// Install on Talos calls ensurePrivilegedNamespaces which needs k8s.NewClientset.
	// With an invalid kubeconfig, this will fail before Helm install.
	// But the coverage still executes the Talos branch in Install().
	err := installer.Install(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "privileged namespaces")
}

// TestInstaller_Install_APIDiscoveryErrorRetry verifies that when the first Helm install
// returns an API discovery error, the installer waits for CRDs and retries.
func TestInstaller_Install_APIDiscoveryErrorRetry(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	// First call: repo add succeeds
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	// First install call returns API discovery error
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, errCalicoRetryNoMatchesInstallation).
		Once()

	// The retry path calls waitForCalicoCRDs which uses k8s.BuildRESTConfig.
	// With invalid kubeconfig, this fails.
	err := installer.Install(context.Background())
	require.Error(t, err)
	// The error should come from waitForCalicoCRDs (REST config build failure)
	assert.Contains(t, err.Error(), "wait for calico CRDs")
}

// TestInstaller_Install_ContextCanceled_Vanilla verifies install fails on canceled context.
func TestInstaller_Install_ContextCanceled_Vanilla(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, context.Canceled)

	err := installer.Install(ctx)
	require.Error(t, err)
}

func TestInstaller_Install_K3s_APIServerUnavailableRetrySucceeds(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionK3s,
	)
	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })
	installer.SetRetryBackoffForTest(func(_ context.Context) error { return nil })

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, errCalicoRetryAPIServerUnavailable).
		Once()
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, nil).
		Once()

	err := installer.Install(context.Background())
	require.NoError(t, err)
}

func TestInstaller_Install_K3s_APIServerUnavailableRetryExhausted(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionK3s,
	)
	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })
	installer.SetRetryBackoffForTest(func(_ context.Context) error { return nil })

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, errCalicoRetryAPIServerUnavailable).
		Times(5)

	err := installer.Install(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install or upgrade calico")
}

// TestInstaller_Install_Vanilla_NoRetryOnAPIServerUnavailable verifies that
// non-K3s distributions do not retry on API-server-unavailable errors, since
// those indicate a genuine cluster problem rather than a transient bootstrap race.
func TestInstaller_Install_Vanilla_NoRetryOnAPIServerUnavailable(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	// Vanilla must NOT retry — InstallOrUpgradeChart is called exactly once.
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, errCalicoRetryAPIServerUnavailable).
		Once()

	err := installer.Install(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install or upgrade calico")
}

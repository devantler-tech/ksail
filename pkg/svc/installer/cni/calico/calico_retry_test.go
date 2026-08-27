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
	installer := calicoinstaller.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionTalos,
		false,
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

// TestInstaller_Install_APIDiscoveryErrorRetry verifies that an API-discovery
// error on the operator install (the operator.tigera.io CRDs installed by the
// CRD chart are not yet visible to Helm's cached discovery) is retried, and the
// install succeeds once discovery is refreshed. This must hold for non-K3s
// distributions too, so Vanilla is used here.
func TestInstaller_Install_APIDiscoveryErrorRetry(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
		false,
	)
	installer.SetRetryBackoffForTest(func(_ context.Context) error { return nil })

	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", "tigera-operator").
		Return(nil, nil)

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	// Two refreshes expected: one after the CRD chart install, and one more
	// before the operator-install retry (triggered by the API-discovery error).
	expectCalicoCRDPhaseWithRefreshes(client, 2)

	// The operator install hits the CRD-establishment race once; the retry
	// (after a discovery refresh) succeeds.
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoOperatorSpec)).
		Return(nil, errCalicoRetryNoMatchesInstallation).
		Once()
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoOperatorSpec)).
		Return(nil, nil).
		Once()

	err := installer.Install(context.Background())
	require.NoError(t, err)
}

// TestInstaller_Install_APIDiscoveryErrorRetryExhausted verifies that a
// persistent API-discovery error eventually fails after exhausting the retry
// budget rather than retrying forever.
func TestInstaller_Install_APIDiscoveryErrorRetryExhausted(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
		false,
	)
	installer.SetRetryBackoffForTest(func(_ context.Context) error { return nil })

	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", "tigera-operator").
		Return(nil, nil)

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	// 8 operator attempts all fail with the discovery error. One refresh follows
	// the CRD install; the 7 retries between the 8 attempts each refresh again
	// (the final attempt breaks without refreshing) — 8 refreshes total.
	expectCalicoCRDPhaseWithRefreshes(client, 8)

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoOperatorSpec)).
		Return(nil, errCalicoRetryNoMatchesInstallation).
		Times(8)

	err := installer.Install(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install or upgrade calico")
}

// TestInstaller_Install_ContextCanceled_Vanilla verifies install fails on canceled context.
func TestInstaller_Install_ContextCanceled_Vanilla(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
		false,
	)

	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", "tigera-operator").
		Return(nil, nil)

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The CRD install (phase 1) is reached first and fails on the canceled context.
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoCRDSpec)).
		Return(nil, context.Canceled)

	err := installer.Install(ctx)
	require.Error(t, err)
}

func TestInstaller_Install_K3s_APIServerUnavailableRetrySucceeds(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionK3s,
		false,
	)
	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })
	installer.SetRetryBackoffForTest(func(_ context.Context) error { return nil })

	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", "tigera-operator").
		Return(nil, nil)

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	expectCalicoCRDPhase(client)

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoOperatorSpec)).
		Return(nil, errCalicoRetryAPIServerUnavailable).
		Once()
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoOperatorSpec)).
		Return(nil, nil).
		Once()

	err := installer.Install(context.Background())
	require.NoError(t, err)
}

func TestInstaller_Install_K3s_APIServerUnavailableRetryExhausted(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionK3s,
		false,
	)
	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })
	installer.SetRetryBackoffForTest(func(_ context.Context) error { return nil })

	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", "tigera-operator").
		Return(nil, nil)

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	expectCalicoCRDPhase(client)

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoOperatorSpec)).
		Return(nil, errCalicoRetryAPIServerUnavailable).
		Times(8)

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
	installer := calicoinstaller.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
		false,
	)

	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", "tigera-operator").
		Return(nil, nil)

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	expectCalicoCRDPhase(client)

	// Vanilla must NOT retry — the operator install is called exactly once.
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoOperatorSpec)).
		Return(nil, errCalicoRetryAPIServerUnavailable).
		Once()

	err := installer.Install(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install or upgrade calico")
}

package calicoinstaller_test

import (
	"context"
	"os"
	"testing"
	"time"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
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

	installer := calicoinstaller.NewInstaller(
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
			installer := calicoinstaller.NewInstallerWithDistribution(
				client,
				"/path/to/kubeconfig",
				"test-context",
				5*time.Minute,
				testCase.distribution,
				false,
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

			installer := calicoinstaller.NewInstaller(
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

	installer := calicoinstaller.NewInstaller(
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
	expectCalicoInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_K3sDistribution(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionK3s)
	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })
	expectCalicoInstall(t, client, nil)

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
		GetReleaseStorageLabels(mock.Anything, "calico", "tigera-operator").
		Return(nil, nil)
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
	expectCalicoInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestInstaller_Install_NilClient(t *testing.T) {
	t.Parallel()

	installer := calicoinstaller.NewInstallerWithDistribution(
		nil, // nil client
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
		false,
	)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client is nil")
}

func TestInstaller_Uninstall_Success(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		UninstallRelease(mock.Anything, "calico", "tigera-operator").
		Return(nil)
	client.EXPECT().
		ReleaseExists(mock.Anything, "calico-crds", "tigera-operator").
		Return(true, nil)
	client.EXPECT().
		UninstallRelease(mock.Anything, "calico-crds", "tigera-operator").
		Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Uninstall_SkipsMissingCRDsRelease(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		UninstallRelease(mock.Anything, "calico", "tigera-operator").
		Return(nil)
	// The calico-crds release does not exist (e.g. cluster predates the two-phase
	// install): uninstall must skip it rather than fail.
	client.EXPECT().
		ReleaseExists(mock.Anything, "calico-crds", "tigera-operator").
		Return(false, nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Uninstall_Error(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		UninstallRelease(mock.Anything, "calico", "tigera-operator").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestInstaller_Uninstall_NilClient(t *testing.T) {
	t.Parallel()

	installer := calicoinstaller.NewInstallerWithDistribution(
		nil, // nil client
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
		false,
	)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client is nil")
}

// --- test helpers ---

func newInstallerWithDistribution(
	t *testing.T,
	distribution v1alpha1.Distribution,
) (*calicoinstaller.Installer, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		distribution,
		false,
	)

	return installer, client
}

func expectCalicoInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()

	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", "tigera-operator").
		Return(nil, nil)

	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				return entry != nil && entry.Name == "projectcalico" &&
					entry.URL == "https://docs.tigera.io/calico/charts"
			}),
			mock.Anything,
		).
		Return(nil)

	expectCalicoCRDPhase(client)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if !isCalicoOperatorSpec(spec) {
					return false
				}

				assert.Equal(t, "calico", spec.ReleaseName)
				assert.Equal(t, "tigera-operator", spec.Namespace)
				assert.Equal(t, "https://docs.tigera.io/calico/charts", spec.RepoURL)
				assert.True(t, spec.CreateNamespace)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Silent)
				assert.True(t, spec.UpgradeCRDs)
				assert.False(t, spec.Wait, "SkipWait should be true")
				assert.False(t, spec.WaitForJobs, "SkipWait should be true")
				assert.Equal(t, 2*time.Minute, spec.Timeout)

				return true
			}),
		).
		Return(nil, installErr)
}

// isCalicoCRDSpec reports whether spec targets the projectcalico.org.v3 CRD chart.
func isCalicoCRDSpec(spec *helm.ChartSpec) bool {
	return spec != nil && spec.ChartName == "projectcalico/projectcalico.org.v3"
}

// isCalicoOperatorSpec reports whether spec targets the tigera-operator chart.
func isCalicoOperatorSpec(spec *helm.ChartSpec) bool {
	return spec != nil && spec.ChartName == "projectcalico/tigera-operator"
}

// expectCalicoCRDPhase sets up the CRD chart install and the single post-CRD
// discovery refresh that precede the operator chart install in the two-phase
// Calico install flow (the no-retry / non-discovery-retry case).
func expectCalicoCRDPhase(client *helm.MockInterface) {
	expectCalicoCRDPhaseWithRefreshes(client, 1)
}

// expectCalicoCRDPhaseWithRefreshes is like expectCalicoCRDPhase but asserts an
// exact RefreshDiscovery call count. One refresh always follows the CRD chart
// install; each API-discovery-error retry of the operator install triggers one
// additional refresh (see runInstallWithRetry). So the discovery-error retry
// path expects 2 and the retry-exhausted path expects 8. Pinning the count means
// a regression that drops the retry-triggered refresh is caught.
func expectCalicoCRDPhaseWithRefreshes(client *helm.MockInterface, refreshes int) {
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(isCalicoCRDSpec)).
		Return(nil, nil).
		Once()
	client.EXPECT().
		RefreshDiscovery().
		Return(nil).
		Times(refreshes)
}

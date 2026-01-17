package calicoinstaller_test

import (
	"context"
	"os"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	calicoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/calico"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

func TestNewCalicoInstaller(t *testing.T) {
	t.Parallel()

	installer := calicoinstaller.NewCalicoInstaller(
		nil,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
	)

	require.NotNil(t, installer, "expected installer to be created")
}

func TestNewCalicoInstallerWithDistribution(t *testing.T) {
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
			installer := calicoinstaller.NewCalicoInstallerWithDistribution(
				client,
				"/path/to/kubeconfig",
				"test-context",
				5*time.Minute,
				testCase.distribution,
			)

			require.NotNil(t, installer, "expected installer to be created")
		})
	}
}

func TestNewCalicoInstaller_WithDifferentTimeout(t *testing.T) {
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

			installer := calicoinstaller.NewCalicoInstaller(
				nil,
				"/path/to/kubeconfig",
				"test-context",
				testCase.timeout,
			)

			require.NotNil(t, installer, "expected installer to be created")
		})
	}
}

func TestNewCalicoInstaller_WithEmptyParams(t *testing.T) {
	t.Parallel()

	installer := calicoinstaller.NewCalicoInstaller(
		nil,
		"",
		"",
		0,
	)

	require.NotNil(t, installer, "expected installer to be created even with empty params")
}

func TestCalicoInstaller_Install_VanillaDistribution(t *testing.T) {
	t.Parallel()

	installer, client := newCalicoInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	expectCalicoInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestCalicoInstaller_Install_K3sDistribution(t *testing.T) {
	t.Parallel()

	installer, client := newCalicoInstallerWithDistribution(t, v1alpha1.DistributionK3s)
	expectCalicoInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestCalicoInstaller_Install_RepoError(t *testing.T) {
	t.Parallel()

	installer, client := newCalicoInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestCalicoInstaller_Install_ChartError(t *testing.T) {
	t.Parallel()

	installer, client := newCalicoInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	expectCalicoInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestCalicoInstaller_Install_NilClient(t *testing.T) {
	t.Parallel()

	installer := calicoinstaller.NewCalicoInstallerWithDistribution(
		nil, // nil client
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client is nil")
}

func TestCalicoInstaller_Uninstall_Success(t *testing.T) {
	t.Parallel()

	installer, client := newCalicoInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		UninstallRelease(mock.Anything, "calico", "tigera-operator").
		Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestCalicoInstaller_Uninstall_Error(t *testing.T) {
	t.Parallel()

	installer, client := newCalicoInstallerWithDistribution(t, v1alpha1.DistributionVanilla)
	client.EXPECT().
		UninstallRelease(mock.Anything, "calico", "tigera-operator").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestCalicoInstaller_Uninstall_NilClient(t *testing.T) {
	t.Parallel()

	installer := calicoinstaller.NewCalicoInstallerWithDistribution(
		nil, // nil client
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client is nil")
}

// --- test helpers ---

func newCalicoInstallerWithDistribution(
	t *testing.T,
	distribution v1alpha1.Distribution,
) (*calicoinstaller.CalicoInstaller, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewCalicoInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		distribution,
	)

	return installer, client
}

func expectCalicoInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()

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

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if spec == nil {
					return false
				}

				assert.Equal(t, "calico", spec.ReleaseName)
				assert.Equal(t, "projectcalico/tigera-operator", spec.ChartName)
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

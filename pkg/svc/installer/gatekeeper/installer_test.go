package gatekeeperinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/gatekeeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := gatekeeperinstaller.NewInstaller(client, "", "", 5*time.Second, false)

	assert.NotNil(t, installer)
}

func TestInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	expectGatekeeperInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

//nolint:paralleltest // Cannot run in parallel — modifies the package-level waitForWebhookReadyFn variable.
func TestInstallSuccessWithWebhookWait(t *testing.T) {
	client := helm.NewMockInterface(t)
	// Use a non-empty kubeconfig to trigger the webhook wait path.
	installer := gatekeeperinstaller.NewInstaller(client, "/fake/kubeconfig", "", 2*time.Minute, false)

	webhookWaitCalled := false

	restore := gatekeeperinstaller.SetWaitForWebhookReadyFn(
		func(_ context.Context, kubeconfig, _ string, _ time.Duration) error {
			webhookWaitCalled = true

			assert.Equal(t, "/fake/kubeconfig", kubeconfig)

			return nil
		},
	)
	defer restore()

	expectGatekeeperInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
	assert.True(t, webhookWaitCalled, "webhook readiness wait should have been called")
}

//nolint:paralleltest // Cannot run in parallel — modifies the package-level waitForWebhookReadyFn variable.
func TestInstallWebhookWaitError(t *testing.T) {
	client := helm.NewMockInterface(t)
	installer := gatekeeperinstaller.NewInstaller(client, "/fake/kubeconfig", "", 2*time.Minute, false)

	restore := gatekeeperinstaller.SetWaitForWebhookReadyFn(
		func(_ context.Context, _, _ string, _ time.Duration) error {
			return assert.AnError
		},
	)
	defer restore()

	expectGatekeeperInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "wait for gatekeeper webhook readiness")
}

func TestInstallRepoError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add gatekeeper repository")
}

func TestInstallChartError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	expectGatekeeperInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install gatekeeper chart")
}

func TestUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().UninstallRelease(mock.Anything, "gatekeeper", "gatekeeper-system").Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "gatekeeper", "gatekeeper-system").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall gatekeeper release")
}

func newInstallerWithDefaults(
	t *testing.T,
) (*gatekeeperinstaller.Installer, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	// Pass empty kubeconfig so the webhook wait is skipped in unit tests.
	installer := gatekeeperinstaller.NewInstaller(client, "", "", 2*time.Minute, false)

	return installer, client
}

func expectGatekeeperInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()

	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				return entry != nil && entry.Name == "gatekeeper" &&
					entry.URL == "https://open-policy-agent.github.io/gatekeeper/charts"
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

				assert.Equal(t, "gatekeeper", spec.ReleaseName)
				assert.Equal(t, "gatekeeper/gatekeeper", spec.ChartName)
				assert.Equal(t, "gatekeeper-system", spec.Namespace)
				assert.Equal(
					t,
					"https://open-policy-agent.github.io/gatekeeper/charts",
					spec.RepoURL,
				)
				assert.True(t, spec.CreateNamespace)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)
				assert.Equal(t, 2*time.Minute, spec.Timeout)

				return true
			}),
		).
		Return(nil, installErr)
}

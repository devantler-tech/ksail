package kyvernoinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	kyvernoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kyverno"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewKyvernoInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := kyvernoinstaller.NewKyvernoInstaller(client, 5*time.Second)

	assert.NotNil(t, installer)
}

func TestKyvernoInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newKyvernoInstallerWithDefaults(t)
	expectKyvernoInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestKyvernoInstallerInstallRepoError(t *testing.T) {
	t.Parallel()

	installer, client := newKyvernoInstallerWithDefaults(t)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add kyverno repository")
}

func TestKyvernoInstallerInstallChartError(t *testing.T) {
	t.Parallel()

	installer, client := newKyvernoInstallerWithDefaults(t)
	expectKyvernoInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install kyverno chart")
}

func TestKyvernoInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newKyvernoInstallerWithDefaults(t)
	client.EXPECT().UninstallRelease(mock.Anything, "kyverno", "kyverno").Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestKyvernoInstallerUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newKyvernoInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "kyverno", "kyverno").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall kyverno release")
}

func newKyvernoInstallerWithDefaults(
	t *testing.T,
) (*kyvernoinstaller.KyvernoInstaller, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := kyvernoinstaller.NewKyvernoInstaller(client, 2*time.Minute)

	return installer, client
}

func expectKyvernoInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()

	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				return entry != nil && entry.Name == "kyverno" &&
					entry.URL == "https://kyverno.github.io/kyverno/"
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

				assert.Equal(t, "kyverno", spec.ReleaseName)
				assert.Equal(t, "kyverno/kyverno", spec.ChartName)
				assert.Equal(t, "kyverno", spec.Namespace)
				assert.Equal(t, "https://kyverno.github.io/kyverno/", spec.RepoURL)
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

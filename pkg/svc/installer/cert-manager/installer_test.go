package certmanagerinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	certmanagerinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cert-manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewCertManagerInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := certmanagerinstaller.NewCertManagerInstaller(client, 5*time.Second)

	assert.NotNil(t, installer)
}

func TestCertManagerInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newCertManagerInstallerWithDefaults(t)
	expectCertManagerInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestCertManagerInstallerInstallRepoError(t *testing.T) {
	t.Parallel()

	installer, client := newCertManagerInstallerWithDefaults(t)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add jetstack repository")
}

func TestCertManagerInstallerInstallChartError(t *testing.T) {
	t.Parallel()

	installer, client := newCertManagerInstallerWithDefaults(t)
	expectCertManagerInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install cert-manager chart")
}

func TestCertManagerInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newCertManagerInstallerWithDefaults(t)
	client.EXPECT().UninstallRelease(mock.Anything, "cert-manager", "cert-manager").Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestCertManagerInstallerUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newCertManagerInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cert-manager", "cert-manager").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall cert-manager release")
}

func newCertManagerInstallerWithDefaults(
	t *testing.T,
) (*certmanagerinstaller.CertManagerInstaller, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := certmanagerinstaller.NewCertManagerInstaller(client, 2*time.Minute)

	return installer, client
}

func expectCertManagerInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()

	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				return entry != nil && entry.Name == "jetstack" &&
					entry.URL == "https://charts.jetstack.io"
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

				assert.Equal(t, "cert-manager", spec.ReleaseName)
				assert.Equal(t, "jetstack/cert-manager", spec.ChartName)
				assert.Equal(t, "cert-manager", spec.Namespace)
				assert.Equal(t, "https://charts.jetstack.io", spec.RepoURL)
				assert.True(t, spec.CreateNamespace)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)
				assert.Equal(t, 2*time.Minute, spec.Timeout)
				assert.Equal(t, map[string]string{"installCRDs": "true"}, spec.SetValues)

				return true
			}),
		).
		Return(nil, installErr)
}

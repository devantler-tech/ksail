package vpainstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	vpainstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/vpa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := vpainstaller.NewInstaller(client, 5*time.Minute)

	assert.NotNil(t, installer)
}

func TestVPAInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newVPAInstallerWithDefaults(t)
	expectVPAInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestVPAInstallerInstallError(t *testing.T) {
	t.Parallel()

	installer, client := newVPAInstallerWithDefaults(t)
	expectVPAInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install vpa")
}

func TestVPAInstallerInstallAddRepositoryError(t *testing.T) {
	t.Parallel()

	installer, client := newVPAInstallerWithDefaults(t)
	expectVPAAddRepository(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add fairwinds-stable repository")
}

func TestVPAInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newVPAInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "vpa", "kube-system").
		Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestVPAInstallerUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newVPAInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "vpa", "kube-system").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall vpa")
}

func newVPAInstallerWithDefaults(t *testing.T) (*vpainstaller.Installer, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := vpainstaller.NewInstaller(client, 5*time.Second)

	return installer, client
}

func expectVPAAddRepository(t *testing.T, client *helm.MockInterface, err error) {
	t.Helper()
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				assert.Equal(t, "fairwinds-stable", entry.Name)
				assert.Equal(t, "https://charts.fairwinds.com/stable", entry.URL)

				return true
			}),
			mock.Anything,
		).
		Return(err)
}

func expectVPAInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()
	expectVPAAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "vpa", spec.ReleaseName)
				assert.Equal(t, "fairwinds-stable/vpa", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://charts.fairwinds.com/stable", spec.RepoURL)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)

				return true
			}),
		).
		Return(nil, installErr)
}

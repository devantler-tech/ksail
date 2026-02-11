package argocdinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	argocdinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/argocd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	timeout := 5 * time.Minute
	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, timeout)

	require.NotNil(t, installer)
}

func TestArgoCDInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newArgoCDInstallerWithDefaults(t)
	expectArgoCDInstall(t, client, nil)

	err := installer.Install(context.Background())
	require.NoError(t, err)
}

func TestArgoCDInstallerInstallError(t *testing.T) {
	t.Parallel()

	installer, client := newArgoCDInstallerWithDefaults(t)
	expectArgoCDInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install Argo CD")
}

func TestArgoCDInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newArgoCDInstallerWithDefaults(t)
	expectArgoCDUninstall(t, client, nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestArgoCDInstallerUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newArgoCDInstallerWithDefaults(t)
	expectArgoCDUninstall(t, client, assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall Argo CD release")
}

func newArgoCDInstallerWithDefaults(
	t *testing.T,
) (*argocdinstaller.Installer, *helm.MockInterface) {
	t.Helper()
	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(
		client,
		5*time.Second,
	)

	return installer, client
}

func expectArgoCDInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "argocd", spec.ReleaseName)
				assert.Equal(t, "oci://ghcr.io/argoproj/argo-helm/argo-cd", spec.ChartName)
				assert.Equal(t, "argocd", spec.Namespace)
				assert.True(t, spec.CreateNamespace)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.UpgradeCRDs)

				return true
			}),
		).
		Return(nil, installErr)
}

func expectArgoCDUninstall(t *testing.T, client *helm.MockInterface, err error) {
	t.Helper()
	client.EXPECT().
		UninstallRelease(mock.Anything, "argocd", "argocd").
		Return(err)
}

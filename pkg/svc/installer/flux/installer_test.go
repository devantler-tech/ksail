package fluxinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail-go/pkg/client/helm"
	fluxinstaller "github.com/devantler-tech/ksail-go/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewFluxInstaller(t *testing.T) {
	t.Parallel()

	timeout := 5 * time.Minute

	client := helm.NewMockInterface(t)
	installer := fluxinstaller.NewFluxInstaller(client, timeout)

	assert.NotNil(t, installer)
}

func TestFluxInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newFluxInstallerWithDefaults(t)
	expectFluxInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestFluxInstallerInstallError(t *testing.T) {
	t.Parallel()

	installer, client := newFluxInstallerWithDefaults(t)
	expectFluxInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install Flux operator")
}

func TestFluxInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newFluxInstallerWithDefaults(t)
	expectFluxUninstall(t, client, nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestFluxInstallerUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newFluxInstallerWithDefaults(t)
	expectFluxUninstall(t, client, assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall flux-operator release")
}

func newFluxInstallerWithDefaults(
	t *testing.T,
) (*fluxinstaller.FluxInstaller, *helm.MockInterface) {
	t.Helper()
	client := helm.NewMockInterface(t)
	installer := fluxinstaller.NewFluxInstaller(
		client,
		5*time.Second,
	)

	return installer, client
}

func expectFluxInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "flux-operator", spec.ReleaseName)
				assert.Equal(
					t,
					"oci://ghcr.io/controlplaneio-fluxcd/charts/flux-operator",
					spec.ChartName,
				)
				assert.Equal(t, "flux-system", spec.Namespace)
				assert.True(t, spec.Silent, "Flux install should silence Helm stderr noise")

				return true
			}),
		).
		Return(nil, installErr)
}

func expectFluxUninstall(t *testing.T, client *helm.MockInterface, err error) {
	t.Helper()
	client.EXPECT().
		UninstallRelease(mock.Anything, "flux-operator", "flux-system").
		Return(err)
}

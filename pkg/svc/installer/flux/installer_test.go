package fluxinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	timeout := 5 * time.Minute

	client := helm.NewMockInterface(t)
	installer := fluxinstaller.NewInstaller(client, timeout, "")

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
) (*fluxinstaller.Installer, *helm.MockInterface) {
	t.Helper()
	client := helm.NewMockInterface(t)
	installer := fluxinstaller.NewInstaller(
		client,
		5*time.Second,
		"",
	)

	return installer, client
}

func expectFluxInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()
	expectNoExistingFluxRelease(t, client)
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

// expectNoExistingFluxRelease sets up GetReleaseStorageLabels to report that no
// flux-operator release storage exists yet, exercising the seed-if-absent path.
func expectNoExistingFluxRelease(t *testing.T, client *helm.MockInterface) {
	t.Helper()
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "flux-operator", "flux-system").
		Return(nil, helm.ErrNoReleaseStorage)
}

// TestFluxInstallerSkipsWhenGitOpsManaged verifies the operator install is
// skipped (deferred) when the flux-operator release Secret is already owned by a
// GitOps controller — InstallOrUpgradeChart must not be called.
func TestFluxInstallerSkipsWhenGitOpsManaged(t *testing.T) {
	t.Parallel()

	installer, client := newFluxInstallerWithDefaults(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "flux-operator", "flux-system").
		Return(map[string]string{"helm.toolkit.fluxcd.io/name": "flux-operator"}, nil)

	// No InstallOrUpgradeChart expectation: the mock fails the test if it is called.
	err := installer.Install(context.Background())

	require.NoError(t, err)
}

// TestFluxInstallerOperatorVersionOverride verifies a configured operator version
// is used as the chart version instead of the embedded Dockerfile pin.
func TestFluxInstallerOperatorVersionOverride(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := fluxinstaller.NewInstaller(client, 5*time.Second, "0.99.0")
	expectNoExistingFluxRelease(t, client)

	var capturedSpec *helm.ChartSpec

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				capturedSpec = spec

				return true
			}),
		).
		Return(nil, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
	require.NotNil(t, capturedSpec)
	assert.Equal(t, "0.99.0", capturedSpec.Version)
}

func expectFluxUninstall(t *testing.T, client *helm.MockInterface, err error) {
	t.Helper()
	client.EXPECT().
		UninstallRelease(mock.Anything, "flux-operator", "flux-system").
		Return(err)
}

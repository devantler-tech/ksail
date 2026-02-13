package metallbinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	metallbinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metallb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	installer, _ := newInstallerWithDefaults(t)

	assert.NotNil(t, installer)
}

func TestNewInstaller_CustomIPRange(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"10.0.0.100-10.0.0.200",
	)

	assert.NotNil(t, installer)
}

func TestUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)

	client.EXPECT().
		UninstallRelease(mock.Anything, "metallb", "metallb-system").
		Return(nil).
		Once()

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)

	client.EXPECT().
		UninstallRelease(mock.Anything, "metallb", "metallb-system").
		Return(assert.AnError).
		Once()

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall metallb release")
	require.ErrorIs(t, err, assert.AnError)
}

func TestUninstallContextCancellation(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)

	client.EXPECT().
		UninstallRelease(mock.MatchedBy(func(ctx context.Context) bool {
			return ctx.Err() != nil
		}), "metallb", "metallb-system").
		Return(context.Canceled).
		Once()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := installer.Uninstall(ctx)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "failed to uninstall metallb release")
}

// Skipped: Install requires a real Kubernetes cluster (ensurePrivilegedNamespace calls k8s.NewClientset).
func TestInstallEnsurePrivilegedNamespace(t *testing.T) {
	t.Parallel()
	t.Skip("requires Kubernetes cluster: ensurePrivilegedNamespace uses k8s.NewClientset")
}

func newInstallerWithDefaults(
	t *testing.T,
) (*metallbinstaller.Installer, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := metallbinstaller.NewInstaller(
		client,
		"~/.kube/config",
		"test-context",
		5*time.Minute,
		"",
	)

	return installer, client
}

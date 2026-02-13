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

func TestInstaller_Uninstall(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)

	client.EXPECT().
		UninstallRelease(mock.Anything, "metallb", "metallb-system").
		Return(nil).
		Once()

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Uninstall_HelmError(t *testing.T) {
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

func TestInstaller_Uninstall_ContextCancellation(t *testing.T) {
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
	assert.Contains(t, err.Error(), "uninstall")
}

// Skipped: Install requires a real Kubernetes cluster (ensurePrivilegedNamespace calls k8s.NewClientset).
func TestInstaller_Install_EnsurePrivilegedNamespace(t *testing.T) {
	t.Parallel()
	t.Skip("requires Kubernetes cluster: ensurePrivilegedNamespace uses k8s.NewClientset")
}

// Skipped: Install calls ensurePrivilegedNamespace before Helm operations, blocking isolation.
func TestInstaller_Install_HelmLifecycle(t *testing.T) {
	t.Parallel()
	t.Skip("requires Kubernetes cluster: ensurePrivilegedNamespace blocks Helm testing")
}

// Skipped: waitForCRDs uses a dynamic.Interface to poll for CRD registration.
func TestInstaller_Install_CRDWait(t *testing.T) {
	t.Parallel()
	t.Skip("requires dynamic Kubernetes client or test cluster")
}

// Skipped: ensureIPAddressPool uses Server-Side Apply via dynamic client.
func TestInstaller_Install_IPAddressPoolCreation(t *testing.T) {
	t.Parallel()
	t.Skip("requires dynamic Kubernetes client or test cluster")
}

// Skipped: ensureL2Advertisement uses Server-Side Apply via dynamic client.
func TestInstaller_Install_L2AdvertisementCreation(t *testing.T) {
	t.Parallel()
	t.Skip("requires dynamic Kubernetes client or test cluster")
}

// Skipped: Full integration test requiring all Kubernetes APIs.
func TestInstaller_Install_FullSuccess(t *testing.T) {
	t.Parallel()
	t.Skip("requires Kubernetes cluster for full integration test")
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

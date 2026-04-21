package metallbinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	metallbinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metallb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	installer, _ := newInstallerWithDefaults(t)

	assert.NotNil(t, installer)
}

func TestNewInstaller_CustomIPRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ipRange string
	}{
		{
			name:    "range format",
			ipRange: "10.0.0.100-10.0.0.200",
		},
		{
			name:    "single IP",
			ipRange: "192.168.1.50",
		},
		{
			name:    "CIDR notation",
			ipRange: "10.96.0.0/24",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			installer := metallbinstaller.NewInstaller(
				client,
				"~/.kube/config",
				"test-context",
				5*time.Minute,
				testCase.ipRange,
			)

			assert.NotNil(t, installer)
		})
	}
}

// Skipped: Install requires a real Kubernetes cluster (ensurePrivilegedNamespace calls k8s.NewClientset).
func TestInstallEnsurePrivilegedNamespace(t *testing.T) {
	t.Parallel()
	t.Skip("requires Kubernetes cluster: ensurePrivilegedNamespace uses k8s.NewClientset")
}

func TestWaitForCRDs_Success(t *testing.T) {
	t.Parallel()

	installer, _ := newInstallerWithDefaults(t)

	// Create fake dynamic client with IPAddressPool CRD registered.
	scheme := runtime.NewScheme()

	// Register the IPAddressPool list kind for the fake client.
	ipAddressPoolGVK := schema.GroupVersionKind{
		Group:   "metallb.io",
		Version: "v1beta1",
		Kind:    "IPAddressPoolList",
	}

	// Create a minimal IPAddressPool list to satisfy the fake client.
	ipAddressPoolList := &unstructured.UnstructuredList{}
	ipAddressPoolList.SetGroupVersionKind(ipAddressPoolGVK)

	dynamicClient := fake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			{Group: "metallb.io", Version: "v1beta1", Resource: "ipaddresspools"}: "IPAddressPoolList",
		},
		ipAddressPoolList,
	)

	ctx := context.Background()
	// Use injectable poll interval/timeout to avoid blocking for 2s.
	err := metallbinstaller.ExportWaitForCRDsWithOptions(
		installer, ctx, dynamicClient, 1*time.Millisecond, 100*time.Millisecond,
	)

	require.NoError(t, err)
}

func TestWaitForCRDs_Timeout(t *testing.T) {
	t.Parallel()

	// Create fake dynamic client WITHOUT the CRD registered.
	// The fake client will panic on List() for unknown resources,
	// so we can't fully test the timeout behavior with the fake client.
	// This test documents the limitation.
	t.Skip("fake dynamic client panics on unknown resources; can't test timeout")
}

func TestWaitForCRDs_ContextCancelled(t *testing.T) {
	t.Parallel()

	// Same limitation as TestWaitForCRDs_Timeout: the fake client panics
	// on unknown resources before we can test context cancellation.
	t.Skip("fake dynamic client panics on unknown resources")
}

func TestWaitForCRDs_UnexpectedError(t *testing.T) {
	t.Parallel()

	// This test simulates non-404 errors (RBAC, network, etc.).
	// The fake client always returns 404 for unknown resources,
	// so we can't easily simulate this scenario without a custom reactor.
	// Documenting the limitation here.
	t.Skip("fake dynamic client always returns 404; can't simulate RBAC/network errors")
}

func TestNewInstaller_IPRangeFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ipRange string
		want    string
	}{
		{
			name:    "default IP range",
			ipRange: "",
			want:    "172.18.255.200-172.18.255.250",
		},
		{
			name:    "custom IP range",
			ipRange: "10.0.0.100-10.0.0.200",
			want:    "10.0.0.100-10.0.0.200",
		},
		{
			name:    "single IP",
			ipRange: "192.168.1.50",
			want:    "192.168.1.50",
		},
		{
			name:    "CIDR notation",
			ipRange: "10.96.0.0/24",
			want:    "10.96.0.0/24",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			installer := metallbinstaller.NewInstaller(
				client,
				"~/.kube/config",
				"test-context",
				5*time.Minute,
				testCase.ipRange,
			)

			assert.Equal(t, testCase.want, metallbinstaller.ExportIPRange(installer))
		})
	}
}

func TestEnsureIPAddressPool_ContextCancelled(t *testing.T) {
	t.Parallel()

	// The fake dynamic client doesn't support Server-Side Apply properly,
	// so we can't test the Apply path. This test documents the limitation.
	t.Skip("fake dynamic client doesn't support Server-Side Apply")
}

func TestEnsureL2Advertisement_Success(t *testing.T) {
	t.Parallel()

	// Same SSA limitation as TestEnsureIPAddressPool.
	t.Skip("fake dynamic client doesn't support Server-Side Apply")
}

func TestEnsureL2Advertisement_ContextCancelled(t *testing.T) {
	t.Parallel()

	t.Skip("fake dynamic client doesn't support Server-Side Apply")
}

func TestEnsureL2Advertisement_ServerError(t *testing.T) {
	t.Parallel()

	t.Skip("fake dynamic client doesn't simulate server errors")
}

func TestInstall_HelmError(t *testing.T) {
	t.Parallel()

	// This test documents that Install() cannot be fully tested without
	// mocking k8s.NewClientset, which is called by ensurePrivilegedNamespace.
	// The Helm Base installer is tested separately in helmutil package.
	t.Skip("Install calls ensurePrivilegedNamespace which requires real k8s client")
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

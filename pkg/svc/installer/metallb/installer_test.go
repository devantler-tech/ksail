package metallbinstaller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	metallbinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metallb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestNewInstaller verifies that the constructor creates an installer with default IP range
// when none is provided. The default range (172.18.255.200-172.18.255.250) is suitable for
// Docker bridge networks.
func TestNewInstaller(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	installer := metallbinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
		"", // Empty IP range should default to 172.18.255.200-172.18.255.250
	)

	assert.NotNil(t, installer, "Installer should be created with default configuration")
}

// TestNewInstaller_CustomIPRange verifies that the constructor accepts and stores
// a custom IP address range. This is useful for environments where the default range
// conflicts with existing network configurations.
func TestNewInstaller_CustomIPRange(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute
	customRange := "10.0.0.100-10.0.0.200"

	installer := metallbinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
		customRange,
	)

	assert.NotNil(t, installer, "Installer should be created with custom IP range")
	// Note: Cannot assert the IP range directly as it's private, but the integration
	// test would verify this by checking the IPAddressPool resource after installation
}

// TestInstaller_Uninstall verifies that Uninstall calls the Helm client's UninstallRelease
// method with the correct release name and namespace.
func TestInstaller_Uninstall(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	mockClient.EXPECT().
		UninstallRelease(mock.Anything, "metallb", "metallb-system").
		Return(nil).
		Once()

	installer := metallbinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
		"",
	)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err, "Uninstall should succeed when Helm uninstall succeeds")
	mockClient.AssertExpectations(t)
}

// TestInstaller_Uninstall_HelmError verifies that Uninstall propagates errors from
// the Helm client and wraps them with context.
func TestInstaller_Uninstall_HelmError(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute
	testErr := errors.New("release not found")

	mockClient.EXPECT().
		UninstallRelease(mock.Anything, "metallb", "metallb-system").
		Return(testErr).
		Once()

	installer := metallbinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
		"",
	)

	err := installer.Uninstall(context.Background())

	require.Error(t, err, "Uninstall should propagate Helm errors")
	assert.Contains(t, err.Error(), "failed to uninstall metallb release", "Error should include context")
	assert.ErrorIs(t, err, testErr, "Original error should be wrapped")
	mockClient.AssertExpectations(t)
}

// TestInstaller_Uninstall_ContextCancellation verifies that Uninstall respects
// context cancellation and returns promptly.
func TestInstaller_Uninstall_ContextCancellation(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	mockClient.EXPECT().
		UninstallRelease(mock.MatchedBy(func(ctx context.Context) bool {
			// Verify the context is cancelled
			return ctx.Err() != nil
		}), "metallb", "metallb-system").
		Return(context.Canceled).
		Once()

	installer := metallbinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
		"",
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := installer.Uninstall(ctx)

	require.Error(t, err, "Uninstall should fail with cancelled context")
	mockClient.AssertExpectations(t)
}

// TestInstaller_Install_EnsurePrivilegedNamespace tests that Install calls
// ensurePrivilegedNamespace to create/update the metallb-system namespace with
// PodSecurity "privileged" labels. This is required for Talos and other distributions
// that enforce PodSecurity Standards by default.
//
// SKIP REASON: Requires real Kubernetes API server or extensive Kubernetes client mocking.
// The ensurePrivilegedNamespace function calls k8s.NewClientset which creates a real
// clientset and attempts to connect to the API server.
//
// To properly test this, we would need:
// 1. A fake Kubernetes API server (e.g., using envtest or kind)
// 2. Or extensive mocking of the kubernetes.Interface and its nested interfaces
//
// COVERAGE IMPACT: This test would cover ensurePrivilegedNamespace (~14 lines)
func TestInstaller_Install_EnsurePrivilegedNamespace(t *testing.T) {
	t.Skip("Requires test Kubernetes cluster - ensurePrivilegedNamespace uses k8s.NewClientset")
}

// TestInstaller_Install_HelmLifecycle tests that Install calls the Helm repository add
// and chart install/upgrade operations in the correct order with the correct parameters.
//
// SKIP REASON: Cannot isolate Helm calls from Kubernetes calls. The Install method
// calls ensurePrivilegedNamespace before any Helm operations, which requires a real
// Kubernetes API server.
//
// COVERAGE IMPACT: This test would cover the Helm Base.Install call (~3 lines in Install method)
func TestInstaller_Install_HelmLifecycle(t *testing.T) {
	t.Skip("Requires test Kubernetes cluster - ensurePrivilegedNamespace blocks Helm testing")
}

// TestInstaller_Install_CRDWait tests the waitForCRDs polling logic that waits for
// MetalLB CRDs to be registered before creating IPAddressPool and L2Advertisement resources.
//
// SKIP REASON: Requires dynamic Kubernetes client that can list CRDs. The waitForCRDs
// function uses a dynamic.Interface to poll for CRD registration.
//
// Test scenarios to cover:
// - Successful CRD registration within timeout
// - Timeout waiting for CRDs (returns error after crdPollTimeout)
// - Unexpected errors (non-404) are returned immediately
// - Context cancellation during polling
//
// COVERAGE IMPACT: This test would cover waitForCRDs (~27 lines)
func TestInstaller_Install_CRDWait(t *testing.T) {
	t.Skip("Requires dynamic Kubernetes client mocking or test cluster")
}

// TestInstaller_Install_IPAddressPoolCreation tests that ensureIPAddressPool creates
// or updates the default-pool IPAddressPool with the configured IP range using
// Server-Side Apply.
//
// SKIP REASON: Requires dynamic Kubernetes client for Server-Side Apply operations.
//
// Test scenarios to cover:
// - Creating a new IPAddressPool with default IP range (172.18.255.200-172.18.255.250)
// - Creating a new IPAddressPool with custom IP range
// - Updating existing IPAddressPool (Server-Side Apply should merge)
// - Apply errors are propagated correctly
//
// COVERAGE IMPACT: This test would cover ensureIPAddressPool (~20 lines)
func TestInstaller_Install_IPAddressPoolCreation(t *testing.T) {
	t.Skip("Requires dynamic Kubernetes client mocking or test cluster")
}

// TestInstaller_Install_L2AdvertisementCreation tests that ensureL2Advertisement creates
// or updates the default-l2-advert L2Advertisement referencing the default-pool using
// Server-Side Apply.
//
// SKIP REASON: Requires dynamic Kubernetes client for Server-Side Apply operations.
//
// Test scenarios to cover:
// - Creating a new L2Advertisement
// - Updating existing L2Advertisement (Server-Side Apply should merge)
// - Apply errors are propagated correctly
//
// COVERAGE IMPACT: This test would cover ensureL2Advertisement (~18 lines)
func TestInstaller_Install_L2AdvertisementCreation(t *testing.T) {
	t.Skip("Requires dynamic Kubernetes client mocking or test cluster")
}

// TestInstaller_Install_FullSuccess tests the complete installation flow from start
// to finish, verifying all steps execute correctly.
//
// SKIP REASON: Full integration test requiring a real Kubernetes cluster.
//
// Test flow:
// 1. Create privileged namespace
// 2. Add Helm repository
// 3. Install MetalLB chart
// 4. Wait for CRDs
// 5. Create IPAddressPool
// 6. Create L2Advertisement
// 7. Verify all resources exist
//
// COVERAGE IMPACT: This test would cover the entire Install method (~20 lines)
// and validate all integration points.
func TestInstaller_Install_FullSuccess(t *testing.T) {
	t.Skip("Requires test Kubernetes cluster for full integration test")
}

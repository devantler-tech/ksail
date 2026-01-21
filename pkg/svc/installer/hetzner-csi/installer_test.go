package hetznercsiinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/hetzner-csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewHetznerCSIInstaller(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	assert.NotNil(t, installer)
}

func TestHetznerCSIInstaller_Install(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	// Set the HCLOUD_TOKEN environment variable for the test
	t.Setenv("HCLOUD_TOKEN", "test-token")

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	// Note: This will fail in the actual test environment because we don't have a real Kubernetes cluster
	// The test is mainly to verify the interface and basic flow
	err := installer.Install(context.Background())

	// We expect an error here because we can't actually create the secret without a real cluster
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create hetzner secret")
}

func TestHetznerCSIInstaller_Install_NoToken(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)

	err := installer.Install(context.Background())

	// Should fail because HCLOUD_TOKEN is not set
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HCLOUD_TOKEN is not set")
}

func TestHetznerCSIInstaller_Uninstall(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	// Mock UninstallRelease call
	mockClient.EXPECT().
		UninstallRelease(mock.Anything, "hcloud-csi", "kube-system").
		Return(nil).
		Once()

	installer := hetznercsiinstaller.NewHetznerCSIInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)
	err := installer.Uninstall(context.Background())

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

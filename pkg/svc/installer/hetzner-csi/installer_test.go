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

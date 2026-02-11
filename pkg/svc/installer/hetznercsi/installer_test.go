package hetznercsiinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/hetznercsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	timeout := 5 * time.Minute

	installer := hetznercsiinstaller.NewInstaller(
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

	installer := hetznercsiinstaller.NewInstaller(
		mockClient,
		"~/.kube/config",
		"test-context",
		timeout,
	)
	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}
